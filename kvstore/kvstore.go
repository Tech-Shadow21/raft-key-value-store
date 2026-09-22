// Package kvstore implements a linearizable key-value service replicated
// via raft.Raft. It knows nothing about consensus mechanics — it only
// Start()s commands and reacts to raft.ApplyMsg — which is the whole point
// of the layering (see docs/DESIGN.md).
package kvstore

import (
	"encoding/gob"
	"sync"
	"time"

	"raftkv/raft"
	"raftkv/transport"
)

func init() {
	// Op flows through raft.LogEntry.Command (interface{}) and therefore
	// through gob encoding on both the in-memory test transport and real
	// persistence; gob requires concrete types behind an interface to be
	// registered explicitly.
	gob.Register(Op{})
}

type OpType string

const (
	OpGet    OpType = "Get"
	OpPut    OpType = "Put"
	OpAppend OpType = "Append"
)

// Op is what gets appended to the Raft log. ClientId/Seq give exactly-once
// semantics: a client retries with the same Seq until it succeeds, and the
// server only applies a given (ClientId, Seq) once.
type Op struct {
	Type     OpType
	Key      string
	Value    string
	ClientId int64
	Seq      int64
}

const applyTimeout = 2 * time.Second

type pendingResult struct {
	term  int
	value string
	err   Err
	done  chan struct{}
}

type Server struct {
	mu      sync.Mutex
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32

	data map[string]string
	// lastSeq/lastReply de-duplicate client requests: a client's Nth
	// request is only ever applied once, even if the client retries after
	// a timeout because its original request actually did commit.
	lastSeq   map[int64]int64
	lastReply map[int64]string

	pending map[int]*pendingResult // log index -> waiter

	persister    raft.Persister
	maxRaftState int // -1 disables snapshotting
}

// StartServer wires up a Server backed by a fresh raft.Raft over ends, and
// begins applying committed entries in the background. maxRaftState <= 0
// disables log-size-triggered snapshotting.
func StartServer(ends []transport.ClientEnd, me int, persister raft.Persister, maxRaftState int) *Server {
	applyCh := make(chan raft.ApplyMsg, 1000)
	kv := &Server{
		applyCh:      applyCh,
		data:         map[string]string{},
		lastSeq:      map[int64]int64{},
		lastReply:    map[int64]string{},
		pending:      map[int]*pendingResult{},
		persister:    persister,
		maxRaftState: maxRaftState,
	}
	kv.rf = raft.Make(ends, me, persister, applyCh)
	kv.restoreSnapshot(persister.ReadSnapshot())
	go kv.applyLoop()
	return kv
}

func (kv *Server) Raft() *raft.Raft { return kv.rf }

type Err string

const (
	OK             Err = "OK"
	ErrNoKey       Err = "ErrNoKey"
	ErrWrongLeader Err = "ErrWrongLeader"
	ErrTimeout     Err = "ErrTimeout"
)

type GetArgs struct {
	Key      string
	ClientId int64
	Seq      int64
}
type GetReply struct {
	Err   Err
	Value string
}

type PutAppendArgs struct {
	Key      string
	Value    string
	Op       OpType // OpPut or OpAppend
	ClientId int64
	Seq      int64
}
type PutAppendReply struct {
	Err Err
}

func (kv *Server) Get(args *GetArgs, reply *GetReply) error {
	value, err := kv.submit(Op{Type: OpGet, Key: args.Key, ClientId: args.ClientId, Seq: args.Seq})
	reply.Err = err
	reply.Value = value
	return nil
}

func (kv *Server) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {
	_, err := kv.submit(Op{Type: args.Op, Key: args.Key, Value: args.Value, ClientId: args.ClientId, Seq: args.Seq})
	reply.Err = err
	return nil
}

// submit appends op to the Raft log (if we're the leader) and blocks until
// it's applied, timed out, or we lose leadership for the term we proposed
// it in — implementing linearizability: even a Get goes through the log,
// so a partitioned-away former leader can never answer from stale local
// state.
func (kv *Server) submit(op Op) (string, Err) {
	index, term, isLeader := kv.rf.Start(op)
	if !isLeader {
		return "", ErrWrongLeader
	}

	kv.mu.Lock()
	res := &pendingResult{term: term, done: make(chan struct{})}
	kv.pending[index] = res
	kv.mu.Unlock()

	select {
	case <-res.done:
		kv.mu.Lock()
		delete(kv.pending, index)
		kv.mu.Unlock()
		return res.value, res.err
	case <-time.After(applyTimeout):
		kv.mu.Lock()
		delete(kv.pending, index)
		kv.mu.Unlock()
		return "", ErrTimeout
	}
}

func (kv *Server) applyLoop() {
	for m := range kv.applyCh {
		if m.SnapshotValid {
			kv.mu.Lock()
			kv.restoreSnapshot(m.Snapshot)
			kv.mu.Unlock()
			continue
		}
		if !m.CommandValid {
			continue
		}
		op := m.Command.(Op)

		kv.mu.Lock()
		var value string
		var errResult Err = OK

		if op.Type == OpGet {
			if v, ok := kv.data[op.Key]; ok {
				value = v
			} else {
				errResult = ErrNoKey
			}
		} else {
			if kv.lastSeq[op.ClientId] < op.Seq {
				switch op.Type {
				case OpPut:
					kv.data[op.Key] = op.Value
				case OpAppend:
					kv.data[op.Key] += op.Value
				}
				kv.lastSeq[op.ClientId] = op.Seq
			}
		}

		if waiter, ok := kv.pending[m.CommandIndex]; ok {
			if waiter.term == m.CommandTerm {
				waiter.value = value
				waiter.err = errResult
			} else {
				waiter.err = ErrWrongLeader
			}
			close(waiter.done)
			delete(kv.pending, m.CommandIndex)
		}

		if kv.maxRaftState > 0 && kv.persister.RaftStateSize() >= kv.maxRaftState {
			snap := kv.encodeSnapshot()
			kv.mu.Unlock()
			kv.rf.Snapshot(m.CommandIndex, snap)
		} else {
			kv.mu.Unlock()
		}
	}
}
