package main

import (
	"errors"
	"sync"

	"raftkv/kvstore"
	"raftkv/raft"
	"raftkv/transport"
)

var errPoweredOff = errors.New("node is powered off")

// node owns the current kvstore.Server incarnation so the dashboard's demo
// power switch can crash and restart it in-process, the same way the test
// harness's Crash1/Restart1 do: the old Raft is killed, RPCs to it are
// refused (peers see exactly what they'd see from a dead process), and a new
// incarnation is rebuilt purely from what was persisted to disk.
type node struct {
	mu           sync.Mutex
	kv           *kvstore.Server // nil while powered off
	fence        *fencedPersister
	ends         []transport.ClientEnd
	me           int
	persister    raft.Persister
	maxRaftState int
	verbose      bool
}

func newNode(ends []transport.ClientEnd, me int, persister raft.Persister, maxRaftState int, verbose bool) *node {
	n := &node{ends: ends, me: me, persister: persister, maxRaftState: maxRaftState, verbose: verbose}
	n.start()
	return n
}

func (n *node) start() {
	n.fence = &fencedPersister{inner: n.persister}
	n.kv = kvstore.StartServer(n.ends, n.me, n.fence, n.maxRaftState)
	n.kv.Raft().Verbose = n.verbose
}

// current returns the live server, or nil while powered off.
func (n *node) current() *kvstore.Server {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.kv
}

func (n *node) setPower(on bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case on && n.kv == nil:
		n.start()
	case !on && n.kv != nil:
		n.kv.Kill()
		// Goroutines of the killed incarnation can still be mid-RPC; without
		// the fence a late persist from them could overwrite state written by
		// the next incarnation.
		n.fence.close()
		n.kv = nil
	}
}

// raftRPC and kvRPC are what's registered with net/rpc instead of the
// server itself, so the registered receivers stay fixed across restarts.
type raftRPC struct{ n *node }

func (r raftRPC) RequestVote(args *raft.RequestVoteArgs, reply *raft.RequestVoteReply) error {
	kv := r.n.current()
	if kv == nil {
		return errPoweredOff
	}
	return kv.Raft().RequestVote(args, reply)
}

func (r raftRPC) AppendEntries(args *raft.AppendEntriesArgs, reply *raft.AppendEntriesReply) error {
	kv := r.n.current()
	if kv == nil {
		return errPoweredOff
	}
	return kv.Raft().AppendEntries(args, reply)
}

func (r raftRPC) InstallSnapshot(args *raft.InstallSnapshotArgs, reply *raft.InstallSnapshotReply) error {
	kv := r.n.current()
	if kv == nil {
		return errPoweredOff
	}
	return kv.Raft().InstallSnapshot(args, reply)
}

type kvRPC struct{ n *node }

func (k kvRPC) Get(args *kvstore.GetArgs, reply *kvstore.GetReply) error {
	kv := k.n.current()
	if kv == nil {
		return errPoweredOff
	}
	return kv.Get(args, reply)
}

func (k kvRPC) PutAppend(args *kvstore.PutAppendArgs, reply *kvstore.PutAppendReply) error {
	kv := k.n.current()
	if kv == nil {
		return errPoweredOff
	}
	return kv.PutAppend(args, reply)
}

// fencedPersister drops writes once closed, so a killed incarnation can't
// clobber the on-disk state its successor reads and writes.
type fencedPersister struct {
	mu     sync.Mutex
	inner  raft.Persister
	closed bool
}

func (p *fencedPersister) close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *fencedPersister) SaveRaftState(state []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.inner.SaveRaftState(state)
	}
}

func (p *fencedPersister) SaveStateAndSnapshot(state []byte, snapshot []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.inner.SaveStateAndSnapshot(state, snapshot)
	}
}

func (p *fencedPersister) ReadRaftState() []byte { return p.inner.ReadRaftState() }
func (p *fencedPersister) ReadSnapshot() []byte  { return p.inner.ReadSnapshot() }
func (p *fencedPersister) RaftStateSize() int    { return p.inner.RaftStateSize() }
