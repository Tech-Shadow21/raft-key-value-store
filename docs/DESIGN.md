# Design

## 1. Scope

A replicated key-value store: `Get(key)`, `Put(key, value)`, `Append(key, value)`.
Linearizable semantics — a client that receives a successful response for a
write can assume every subsequent read (from any node) sees that write. The
system tolerates the crash (not Byzantine) failure of a minority of nodes and
network partitions, per the Raft paper's fault model.

## 2. Layering

```
 client
   |  Get/Put/Append RPC
   v
 kvstore.Server            <- replicated state machine, one per node
   |  Start(command) / ApplyMsg channel
   v
 raft.Raft                 <- consensus core, one per node
   |  RequestVote / AppendEntries RPC
   v
 transport.Transport        <- pluggable: in-memory (tests) or TCP (prod)
```

`kvstore.Server` never talks to peers directly. It hands commands to its local
`raft.Raft` instance via `Start()`, and learns what has been committed by
reading `ApplyMsg`s off a channel `raft.Raft` feeds as entries reach a
majority. This mirrors the 6.5840 lab architecture (`Raft.Start`, `applyCh`)
deliberately, since that architecture is what the lab's tester expects and
it's a proven separation of concerns: consensus knows nothing about what the
log entries mean.

## 3. Raft core (`raft/`)

### 3.1 State

Each node is one of `Follower`, `Candidate`, `Leader`. Persistent state
(survives crashes, written to `Persister` before replying to any RPC that
changes it):

- `currentTerm int`
- `votedFor int` (peer id, or -1)
- `log []LogEntry` — `{Term int, Index int, Command interface{}}`

Volatile state:

- `commitIndex`, `lastApplied` (all servers)
- `nextIndex[]`, `matchIndex[]` (leaders only, reinitialized on election)

### 3.2 RPCs

- `RequestVote(term, candidateId, lastLogIndex, lastLogTerm) -> (term, voteGranted)`
- `AppendEntries(term, leaderId, prevLogIndex, prevLogTerm, entries[], leaderCommit) -> (term, success, conflictIndex, conflictTerm)`
  used both for log replication and as heartbeats (empty `entries`).
- `InstallSnapshot(term, leaderId, lastIncludedIndex, lastIncludedTerm, data) -> (term)`
  for log compaction, so a slow follower doesn't force the leader to keep an
  unbounded log.

Election safety, leader append-only, log matching, leader completeness, and
state machine safety follow the original Raft paper (Ongaro & Ousterhout,
2014) directly — this implementation doesn't deviate from the paper's
mechanism, only from its pseudocode's exact variable names where Go idiom
differs.

### 3.3 Timing

- Election timeout: randomized in `[300ms, 600ms)` per node, reset on any
  valid heartbeat/RPC from the current leader or on granting a vote.
- Heartbeat interval: `100ms` (well under the minimum election timeout, per
  the paper's guidance of 10x).
- These are constants in `raft/config.go`; the in-memory test transport can
  scale them down for faster tests.

### 3.4 Persistence

`Persister` is an interface (`Save([]byte) error`, `Load() ([]byte, error)`)
with two implementations: `MemoryPersister` (tests — survives a simulated
crash/restart of the same process) and `FilePersister` (production — writes
term/votedFor/log via `encoding/gob` to disk with an fsync before the RPC
reply that depended on it returns, so a crash right after can't lose an
acknowledged vote or entry).

## 4. KV layer (`kvstore/`)

`kvstore.Server` holds an in-memory `map[string]string` plus a
`map[clientId]lastSeq` for exactly-once semantics (clients retry on timeout;
duplicate `(clientId, seq)` commands are detected and answered from a cached
result instead of re-applied).

Flow for a write:

1. Client sends `Put(key, value, clientId, seq)` to a server (any server; if
   it isn't the leader, it replies `ErrWrongLeader` with the last known
   leader hint).
2. Leader calls `raft.Start(cmd)`; gets back `(index, term, isLeader)`.
3. Leader waits on a per-index channel for the command to be applied via
   `ApplyMsg`, up to a timeout. If the term changes underneath it (leader
   lost leadership), it fails the pending request so the client retries.
4. Once applied, the map is mutated and the cached result stored for
   `(clientId, seq)`; the leader replies to the client.

Reads (`Get`) go through the same path (as a no-op-through-the-log read,
i.e. read-index style: appended as a log entry / confirmed via a heartbeat
round to a majority) rather than being served from a leader's local state
directly, which is what rules out stale reads after a leader is partitioned
away and a new leader has already been elected.

## 5. Fault injection

See [TESTING.md](TESTING.md).

## 6. Non-goals (v1)

- Cluster membership changes (joint consensus) — fixed cluster size at boot.
- Byzantine fault tolerance.
- Multi-raft / sharding.
