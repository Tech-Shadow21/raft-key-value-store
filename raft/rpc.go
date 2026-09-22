package raft

// RPC argument/reply types. Field names are exported so encoding/gob (used
// by the persister) and the in-memory transport's reflection-based dispatch
// can both see them.

type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term          int
	Success       bool
	ConflictIndex int
	ConflictTerm  int
	// HasConflict distinguishes "log too short" (ConflictTerm==0) from a
	// genuine term mismatch during fast backtracking.
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

type LogEntry struct {
	Term    int
	Index   int
	Command interface{}
}
