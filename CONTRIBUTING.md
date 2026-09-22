# Contributing

## Running tests

```bash
go test ./...                 # everything
go test ./raft/... -v         # consensus unit + fault-injection tests
go test ./kvstore/... -v      # KV correctness + fault-injection tests
go test ./test/... -run Chaos -v -count=5   # chaos test, several seeds
go test ./... -race           # always run this before sending a change
```

`gofmt -l .` should print nothing; `go vet ./...` should be clean.

## Where things live

See [docs/DESIGN.md](docs/DESIGN.md) for the layering (`raft` → `kvstore` →
`transport`) and [docs/TESTING.md](docs/TESTING.md) for how the
fault-injection harness works. Read both before touching `raft/raft.go` —
almost every line there exists to preserve one of Raft's five safety
properties (election safety, leader append-only, log matching, leader
completeness, state machine safety), and a change that looks like a harmless
simplification can silently violate one of them.

## Ground rules for changes to `raft/` or `kvstore/`

- Any change to election, replication, persistence, or commit-index logic
  must keep `go test ./raft/... -race -count=10` passing — consensus bugs
  are often timing-dependent and won't show up on a single run.
- If you add a new RPC or a new field to an existing RPC struct, update
  both the in-memory dispatch path (it's reflection-based and doesn't care
  about field names) and confirm `cmd/kvserver`'s real TCP path still works
  (`net/rpc` has stricter method-signature requirements than the in-memory
  transport does — see `transport/tcp.go`'s doc comment).
- New commands added to `kvstore.Op` must be registered with
  `gob.Register` (see `kvstore/kvstore.go`'s `init()`) or the in-memory
  transport and file persistence will fail at encode time, not compile
  time.
- Prefer adding a new fault-injection scenario in `raft/raft_test.go`,
  `kvstore/kvstore_test.go`, or `test/chaos_test.go` over hand-verifying a
  fix — the whole point of this project is that correctness is
  demonstrated by tests.

## Style

Standard Go: `gofmt`, short receiver names, comments explain *why* not
*what*. No comment blocks restating what the code obviously does.
