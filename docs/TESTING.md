# Fault-injection test harness

Modeled on the 6.5840 `config.go` tester: tests don't mock Raft's logic,
they run real `raft.Raft` instances wired together over an in-memory
`transport.Network` that the test controls, and assert on observable
behavior (what gets committed, what leaders think they are).

## `transport.Network` (transport/memory.go)

An in-memory RPC fabric shared by all peers in a test:

- `Connect(id)` / `Disconnect(id)` — simulate a node losing all network
  connectivity (partition of one).
- `SetReliable(bool)` — when unreliable, RPCs are dropped or delayed
  randomly (models packet loss / a slow link).
- `SetLongDelay(bool)` — occasionally hold a reply well past the sender's
  timeout, to test that stale RPC replies are ignored.
- Every RPC is delivered on its own goroutine with a random delay in
  `[1ms, 27ms]` when reliable, so tests exercise real concurrency, not a
  lockstep simulation.

## `test.Cluster` (test/cluster.go)

Wraps N `raft.Raft` (or N `kvstore.Server`) instances plus their `Network`
and `Persister`s:

- `Crash1(i)` — stop a server's goroutines and RPC handlers as if the
  process died; its `Persister` (state written to "disk") survives.
- `Restart1(i)` — bring it back up reading from the same `Persister`,
  exactly like a process restart.
- `Partition(part1, part2 []int)` — disconnect two groups from each other
  (each side can still talk within itself).
- `CheckOneLeader()` — polls until exactly one server in a connected
  majority believes it's leader, fails the test if that never converges.
- `CheckNoLeader()` — asserts no server can win an election (used when a
  majority is partitioned away).
- `Nagree(index)` / `CheckCommitted(...)` — asserts a supermajority of
  *connected* servers agree on the command at a given log index, and that
  no server disagrees (safety, not just liveness).

## Fault scenarios exercised (`raft/*_test.go`, `kvstore/*_test.go`)

| Scenario | What it proves |
|---|---|
| Kill the leader mid-write | A new leader is elected and the write still commits (if it reached a majority) or is cleanly never acknowledged (if it didn't). |
| Partition leader from majority | Old leader steps down (sees higher term on rejoin); no split-brain double-commit at the same index. |
| Repeated crash/restart of random servers under an unreliable network | `TestFigure8`-style: log never diverges at a committed index across any two servers, no matter the interleaving. |
| Client retries after a timeout | Exactly-once: a retried `Put` isn't applied twice (checked via the clientId/seq de-dup table). |
| Snapshot + lagging follower rejoin | Follower catches up via `InstallSnapshot` instead of never catching up, and its applied state matches. |
| Chaos test (`test/chaos_test.go`, phase 6) | Randomized sequence of the above, many seeds, invariant checked continuously: any value a client was told is committed is never subsequently missing or different on read. |

## Running just the fault-injection tests

```bash
go test ./raft/... -run Fault -v
go test ./kvstore/... -run Partition -v
go test ./test/... -run Chaos -v -count=20   # multiple random seeds
```
