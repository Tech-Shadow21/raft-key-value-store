package test

import (
	"fmt"
	"testing"
	"time"

	"raftkv/kvstore"
	"raftkv/raft"
	"raftkv/transport"
)

// KVCluster is the kvstore analogue of Cluster: N kvstore.Server instances
// over an in-memory Network, with the same crash/restart/partition knobs,
// plus MakeClerk for driving real Get/Put/Append traffic against it.
type KVCluster struct {
	t            *testing.T
	net          *transport.Network
	n            int
	servers      []*kvstore.Server
	persister    []*raft.MemoryPersister
	connected    []bool
	endnames     [][]string
	clerkEnds    [][]transport.ClientEnd // one full set of ends per Clerk created
	maxRaftState int
}

func MakeKVCluster(t *testing.T, n int, unreliable bool, maxRaftState int) *KVCluster {
	c := &KVCluster{
		t:            t,
		net:          transport.MakeNetwork(),
		n:            n,
		servers:      make([]*kvstore.Server, n),
		persister:    make([]*raft.MemoryPersister, n),
		connected:    make([]bool, n),
		endnames:     make([][]string, n),
		maxRaftState: maxRaftState,
	}
	c.net.SetReliable(!unreliable)
	for i := 0; i < n; i++ {
		c.startServer(i)
	}
	for i := 0; i < n; i++ {
		c.connect(i)
	}
	return c
}

func (c *KVCluster) startServer(i int) {
	if c.persister[i] == nil {
		c.persister[i] = raft.MakeMemoryPersister()
	}
	ends := make([]transport.ClientEnd, c.n)
	c.endnames[i] = make([]string, c.n)
	for j := 0; j < c.n; j++ {
		name := fmt.Sprintf("kv-%d-%d-%d", i, j, time.Now().UnixNano())
		c.endnames[i][j] = name
		end := c.net.MakeEnd(name)
		ends[j] = end
		c.net.Connect(name, j)
		c.net.Enable(name, false)
	}

	kv := kvstore.StartServer(ends, i, c.persister[i], c.maxRaftState)
	c.servers[i] = kv

	srv := transport.MakeServer()
	srv.RegisterName("KVServer", kv)
	srv.RegisterName("Raft", kv.Raft())
	c.net.AddServer(i, srv)
}

func (c *KVCluster) connect(i int) {
	c.connected[i] = true
	for j := 0; j < c.n; j++ {
		if c.connected[j] {
			c.net.Enable(c.endnames[i][j], true)
			c.net.Enable(c.endnames[j][i], true)
		}
	}
}

func (c *KVCluster) Connect(i int) { c.connect(i) }

func (c *KVCluster) Disconnect(i int) {
	c.connected[i] = false
	for j := 0; j < c.n; j++ {
		if c.endnames[i] != nil {
			c.net.Enable(c.endnames[i][j], false)
		}
		if c.endnames[j] != nil {
			c.net.Enable(c.endnames[j][i], false)
		}
	}
}

func (c *KVCluster) Partition(part1, part2 []int) {
	inPart1 := map[int]bool{}
	for _, i := range part1 {
		inPart1[i] = true
	}
	for _, i := range part1 {
		for j := 0; j < c.n; j++ {
			connect := inPart1[j]
			c.net.Enable(c.endnames[i][j], connect)
			c.net.Enable(c.endnames[j][i], connect)
		}
	}
	for _, i := range part2 {
		for j := 0; j < c.n; j++ {
			connect := !inPart1[j]
			c.net.Enable(c.endnames[i][j], connect)
			c.net.Enable(c.endnames[j][i], connect)
		}
	}
}

func (c *KVCluster) Crash1(i int) {
	c.Disconnect(i)
	c.net.DeleteServer(i)
	if c.servers[i] != nil {
		c.servers[i].Kill()
	}
}

func (c *KVCluster) Restart1(i int) {
	c.startServer(i)
	c.connect(i)
}

func (c *KVCluster) Shutdown() {
	for i := 0; i < c.n; i++ {
		if c.servers[i] != nil {
			c.servers[i].Kill()
		}
	}
	c.net.Cleanup()
}

func (c *KVCluster) SetUnreliable(u bool) { c.net.SetReliable(!u) }

// MakeClerk returns a fresh Clerk wired to every server in the cluster
// (including currently-disconnected ones — the Clerk just won't get a
// reply from those until they're reconnected).
func (c *KVCluster) MakeClerk() *kvstore.Clerk {
	ends := make([]transport.ClientEnd, c.n)
	for j := 0; j < c.n; j++ {
		name := fmt.Sprintf("kv-clerk-%d-%d", j, time.Now().UnixNano())
		end := c.net.MakeEnd(name)
		c.net.Connect(name, j)
		c.net.Enable(name, true)
		ends[j] = end
	}
	return kvstore.MakeClerk(ends)
}

func (c *KVCluster) RaftStateSize(i int) int {
	return c.persister[i].RaftStateSize()
}
