# Roadmap

Phased like the MIT 6.5840 labs (each phase has its own test file so
correctness is checked incrementally, not just at the end).

- [x] **Phase 0 — scaffolding.** Module layout, docs, in-memory transport
      with configurable delay/drop/partition, test harness (`test/cluster.go`)
      that can start/stop/crash/partition N raft peers.
- [x] **Phase 1 (~2A) — leader election.** `RequestVote`, term handling,
      randomized election timeouts, heartbeats. Tests: single leader elected,
      re-election after leader crash, no leader with no majority.
- [x] **Phase 2 (~2B) — log replication.** `AppendEntries` with entries,
      `Start()`, commit index advancement, `ApplyMsg` delivery. Tests: basic
      agreement, agreement despite a minority of failures, no agreement
      without a majority, RPC byte-count sanity.
- [x] **Phase 3 (~2C) — persistence.** Save/restore `currentTerm`,
      `votedFor`, `log` across simulated restarts. Tests: leader/follower
      restart mid-replication, unreliable network + crashes combined (Figure 8
      scenario).
- [x] **Phase 4 (~2D) — log compaction / snapshots.** `InstallSnapshot`,
      trimming the in-memory log. Tests: snapshot after crash, lagging
      follower catches up via snapshot instead of a huge `AppendEntries`.
- [x] **Phase 5 (~3A/3B) — KV service.** `kvstore.Server` on top of `raft.Raft`,
      exactly-once client semantics, linearizable `Get`. Tests: basic put/get,
      concurrent clients, operations during partitions/crashes, snapshotting
      the KV state.
- [x] **Phase 6 — fault-injection harness hardening.** Randomized chaos
      test (`test/chaos_test.go`) that runs concurrent clients against a
      5-node cluster while randomly crashing/restarting nodes, asserting
      the invariant: no client ever sees a committed write disappear.
- [x] **Phase 7 — real transport + CLI.** TCP transport (`transport/tcp.go`),
      `cmd/kvserver` daemon, `cmd/kvctl` CLI. Verified by hand against a
      real 3-process local cluster: `kill -9` on the leader mid-session,
      confirmed a new leader was elected (term advanced), confirmed no
      write made before the kill was lost, confirmed the killed node
      rejoined as a correct-term follower (not a stale leader) and served
      reads consistent with the rest of the cluster after restart.
- [x] **Phase 8 — polish.** Leader-change and commit-latency logging
      (`raft.Raft.Verbose`, on by default in `cmd/kvserver`), a Mermaid
      architecture diagram in the README, and [CONTRIBUTING.md](../CONTRIBUTING.md).

All phases complete. Every property in the original ask is implemented and
verified by tests, not just asserted: leader election, WAL replication,
crash survival, consistent reads/writes, and a fault-injection harness that
proves it (`go test ./...`, clean under `-race`, plus a chaos test and a
manual `kill -9` against a real 3-node TCP cluster).

Deliberately out of scope (see docs/DESIGN.md §6): cluster membership
changes, Byzantine fault tolerance, multi-raft/sharding.
