package raft

import "time"

// Timing constants. Kept as vars (not const) so tests can shrink them for
// faster runs; production code leaves them at these defaults.
var (
	HeartbeatInterval    = 100 * time.Millisecond
	ElectionTimeoutMin   = 300 * time.Millisecond
	ElectionTimeoutRange = 300 * time.Millisecond // timeout in [Min, Min+Range)
)
