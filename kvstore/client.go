package kvstore

import (
	"crypto/rand"
	"math/big"
	"sync/atomic"
	"time"

	"raftkv/transport"
)

// Clerk is the client library: it doesn't know which server is the leader,
// so it round-robins until one accepts (or reports OK), retrying on
// ErrWrongLeader / ErrTimeout / a dropped RPC, per the standard Raft-client
// pattern.
type Clerk struct {
	servers    []transport.ClientEnd
	clientId   int64
	seq        int64
	lastLeader int
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	n, _ := rand.Int(rand.Reader, max)
	return n.Int64()
}

func MakeClerk(servers []transport.ClientEnd) *Clerk {
	return &Clerk{
		servers:  servers,
		clientId: nrand(),
	}
}

func (ck *Clerk) nextSeq() int64 {
	return atomic.AddInt64(&ck.seq, 1)
}

func (ck *Clerk) Get(key string) string {
	seq := ck.nextSeq()
	args := &GetArgs{Key: key, ClientId: ck.clientId, Seq: seq}
	for {
		for i := 0; i < len(ck.servers); i++ {
			srv := (ck.lastLeader + i) % len(ck.servers)
			var reply GetReply
			ok := ck.servers[srv].Call("KVServer.Get", args, &reply)
			if ok && (reply.Err == OK || reply.Err == ErrNoKey) {
				ck.lastLeader = srv
				return reply.Value
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (ck *Clerk) PutAppend(key, value string, op OpType) {
	seq := ck.nextSeq()
	args := &PutAppendArgs{Key: key, Value: value, Op: op, ClientId: ck.clientId, Seq: seq}
	for {
		for i := 0; i < len(ck.servers); i++ {
			srv := (ck.lastLeader + i) % len(ck.servers)
			var reply PutAppendReply
			ok := ck.servers[srv].Call("KVServer.PutAppend", args, &reply)
			if ok && reply.Err == OK {
				ck.lastLeader = srv
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (ck *Clerk) Put(key, value string)    { ck.PutAppend(key, value, OpPut) }
func (ck *Clerk) Append(key, value string) { ck.PutAppend(key, value, OpAppend) }
