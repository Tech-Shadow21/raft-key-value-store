// Package test provides the fault-injection harness used by raft and
// kvstore tests: a Cluster of N raft.Raft peers wired together over an
// in-memory transport.Network that the test can crash, restart, and
// partition on demand. Modeled on MIT 6.5840's config.go.
package test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"raftkv/raft"
	"raftkv/transport"
)

type Cluster struct {
	mu        sync.Mutex
	t         *testing.T
	net       *transport.Network
	n         int
	rafts     []*raft.Raft
	persister []*raft.MemoryPersister
	connected []bool
	endnames  [][]string // endnames[i][j] = name of the end i uses to talk to j
	applyErr  []string   // set if a server's applied log diverges from another's
	logs      []map[int]interface{}
	applyCh   []chan raft.ApplyMsg
	saved     bool
}

func MakeCluster(t *testing.T, n int, unreliable bool) *Cluster {
	c := &Cluster{
		t:         t,
		net:       transport.MakeNetwork(),
		n:         n,
		rafts:     make([]*raft.Raft, n),
		persister: make([]*raft.MemoryPersister, n),
		connected: make([]bool, n),
		endnames:  make([][]string, n),
		applyErr:  make([]string, n),
		logs:      make([]map[int]interface{}, n),
		applyCh:   make([]chan raft.ApplyMsg, n),
	}
	c.net.SetReliable(!unreliable)

	for i := 0; i < n; i++ {
		c.logs[i] = map[int]interface{}{}
		c.startServer(i)
	}
	for i := 0; i < n; i++ {
		c.connect(i)
	}
	return c
}

func (c *Cluster) startServer(i int) {
	if c.persister[i] == nil {
		c.persister[i] = raft.MakeMemoryPersister()
	}

	ends := make([]transport.ClientEnd, c.n)
	c.endnames[i] = make([]string, c.n)
	for j := 0; j < c.n; j++ {
		name := fmt.Sprintf("%d-%d-%d", i, j, time.Now().UnixNano())
		c.endnames[i][j] = name
		end := c.net.MakeEnd(name)
		ends[j] = end
		c.net.Connect(name, j)
		c.net.Enable(name, false)
	}

	c.applyCh[i] = make(chan raft.ApplyMsg, 1000)
	rf := raft.Make(ends, i, c.persister[i], c.applyCh[i])
	c.rafts[i] = rf

	srv := transport.MakeServer()
	srv.RegisterName("Raft", rf)
	c.net.AddServer(i, srv)

	go c.applierWatcher(i, c.applyCh[i])
}

func (c *Cluster) applierWatcher(i int, ch chan raft.ApplyMsg) {
	for m := range ch {
		if !m.CommandValid {
			continue
		}
		c.mu.Lock()
		for j := 0; j < c.n; j++ {
			if j == i {
				continue
			}
			if old, ok := c.logs[j][m.CommandIndex]; ok && !equalCommand(old, m.Command) {
				c.applyErr[i] = fmt.Sprintf("commit index=%d server=%d %v != server=%d %v",
					m.CommandIndex, i, m.Command, j, old)
			}
		}
		c.logs[i][m.CommandIndex] = m.Command
		c.mu.Unlock()
	}
}

func equalCommand(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func (c *Cluster) connect(i int) {
	c.connected[i] = true
	for j := 0; j < c.n; j++ {
		if c.connected[j] {
			c.net.Enable(c.endnames[i][j], true)
			c.net.Enable(c.endnames[j][i], true)
		}
	}
}

func (c *Cluster) Disconnect(i int) {
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

func (c *Cluster) Connect(i int) {
	c.connect(i)
}

// Partition disconnects part1 from part2 entirely (each part stays fully
// connected internally). Useful for the classic "leader gets cut off from
// the majority" fault scenario.
func (c *Cluster) Partition(part1, part2 []int) {
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

// Crash1 simulates server i's process dying: its RPC handler stops
// responding, but its Persister (proxy for its disk) is untouched.
func (c *Cluster) Crash1(i int) {
	c.Disconnect(i)
	c.net.DeleteServer(i)

	c.mu.Lock()
	rf := c.rafts[i]
	c.mu.Unlock()
	if rf != nil {
		rf.Kill()
	}
}

// Restart1 brings server i back up reading from its persisted state, as if
// the OS process had restarted.
func (c *Cluster) Restart1(i int) {
	c.startServer(i)
	c.connect(i)
}

func (c *Cluster) Shutdown() {
	for i := 0; i < c.n; i++ {
		if c.rafts[i] != nil {
			c.rafts[i].Kill()
		}
	}
	c.net.Cleanup()
}

func (c *Cluster) Rafts() []*raft.Raft { return c.rafts }

func (c *Cluster) SetUnreliable(u bool) { c.net.SetReliable(!u) }
func (c *Cluster) SetLongDelays(v bool) { c.net.SetLongDelays(v) }

func (c *Cluster) CheckNoErrors() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.applyErr {
		if e != "" {
			c.t.Fatalf("server %d apply error: %s", i, e)
		}
	}
}

// CheckOneLeader polls until exactly one server believes it's leader for
// the current (highest observed) term, or fails the test.
func (c *Cluster) CheckOneLeader() int {
	for iters := 0; iters < 30; iters++ {
		// Sleep at least one full heartbeat interval before checking, so a
		// just-reconnected stale leader has had a chance to receive a
		// heartbeat from the real leader and step down — otherwise we can
		// observe (and lock in) a transient two-leader snapshot.
		time.Sleep(150 * time.Millisecond)
		leaders := map[int][]int{}
		for i := 0; i < c.n; i++ {
			if !c.connected[i] || c.rafts[i] == nil {
				continue
			}
			term, isLeader := c.rafts[i].GetState()
			if isLeader {
				leaders[term] = append(leaders[term], i)
			}
		}
		lastTermWithLeader := -1
		for t := range leaders {
			if len(leaders[t]) > 1 {
				c.t.Fatalf("term %d has %d (>1) leaders", t, len(leaders[t]))
			}
			if t > lastTermWithLeader {
				lastTermWithLeader = t
			}
		}
		if lastTermWithLeader != -1 {
			return leaders[lastTermWithLeader][0]
		}
	}
	c.t.Fatalf("expected one leader, got none")
	return -1
}

func (c *Cluster) CheckNoLeader() {
	for i := 0; i < c.n; i++ {
		if c.connected[i] && c.rafts[i] != nil {
			_, isLeader := c.rafts[i].GetState()
			if isLeader {
				c.t.Fatalf("server %d claims to be leader with no majority", i)
			}
		}
	}
}

func (c *Cluster) CheckTerms() int {
	term := -1
	for i := 0; i < c.n; i++ {
		if c.connected[i] && c.rafts[i] != nil {
			t, _ := c.rafts[i].GetState()
			if term == -1 {
				term = t
			} else if term != t {
				c.t.Fatalf("servers disagree on term")
			}
		}
	}
	return term
}

// NCommitted returns how many (connected or not) servers have applied the
// given index, and the command they applied there, failing the test if
// any two disagree on what that command was.
func (c *Cluster) NCommitted(index int) (int, interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	var cmd interface{}
	for i := 0; i < c.n; i++ {
		if v, ok := c.logs[i][index]; ok {
			if count > 0 && !equalCommand(cmd, v) {
				c.t.Fatalf("committed values do not match at index %d: %v vs %v", index, cmd, v)
			}
			count++
			cmd = v
		}
	}
	return count, cmd
}

// One submits cmd to whichever server looks like the leader, then waits
// for it to be committed by a majority, returning the index it committed
// at. Fails the test if it doesn't commit within a generous timeout.
func (c *Cluster) One(cmd interface{}, expectedServers int, retry bool) int {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		index := -1
		for si := 0; si < c.n; si++ {
			c.mu.Lock()
			rf := c.rafts[si]
			connected := c.connected[si]
			c.mu.Unlock()
			if rf == nil || !connected {
				continue
			}
			i, _, isLeader := rf.Start(cmd)
			if isLeader {
				index = i
				break
			}
		}
		if index != -1 {
			t1 := time.Now()
			for time.Since(t1) < 2*time.Second {
				nd, cmd1 := c.NCommitted(index)
				if nd >= expectedServers && equalCommand(cmd1, cmd) {
					return index
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !retry {
				c.t.Fatalf("one(%v) failed to reach agreement", cmd)
			}
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	c.t.Fatalf("one(%v) failed to reach agreement", cmd)
	return -1
}
