package raft_test

import (
	"testing"
	"time"

	"raftkv/test"
)

// ---- Phase 1: leader election ----

func TestInitialElection(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()

	c.CheckOneLeader()
	term1 := c.CheckTerms()
	time.Sleep(50 * time.Millisecond)
	term2 := c.CheckTerms()
	if term1 != term2 {
		t.Fatalf("term changed without cause: %d -> %d", term1, term2)
	}
	c.CheckOneLeader()
}

func TestReElection(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()

	leader1 := c.CheckOneLeader()
	c.Disconnect(leader1)
	leader2 := c.CheckOneLeader()
	if leader2 == leader1 {
		t.Fatalf("old leader still leading after disconnect")
	}

	c.Connect(leader1)
	leader3 := c.CheckOneLeader()

	// Disconnect the ACTUAL current leader plus one follower, so the lone
	// remaining node is guaranteed to be a follower (it can win at most
	// its own vote, never a majority of 3).
	c.Disconnect(leader3)
	other := (leader3 + 1) % 3
	c.Disconnect(other)
	time.Sleep(300 * time.Millisecond)
	c.CheckNoLeader()

	c.Connect(other)
	c.CheckOneLeader()
}

func TestManyElections(t *testing.T) {
	c := test.MakeCluster(t, 5, false)
	defer c.Shutdown()
	c.CheckOneLeader()

	for i := 0; i < 5; i++ {
		leader := c.CheckOneLeader()
		c.Disconnect(leader)
		other := (leader + 1) % 5
		c.Disconnect(other)
		c.CheckOneLeader()
		c.Connect(other)
		c.Connect(leader)
	}
	c.CheckOneLeader()
}

// ---- Phase 2: log replication ----

func TestBasicAgree(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()
	c.CheckOneLeader()

	for i := 1; i <= 3; i++ {
		nd, _ := c.NCommitted(i)
		if nd > 0 {
			t.Fatalf("index %d already committed before Start", i)
		}
		c.One(i*100, 3, false)
	}
}

func TestFailAgree(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()
	c.CheckOneLeader()

	c.One(1, 3, false)
	leader := c.CheckOneLeader()
	c.Disconnect((leader + 1) % 3)

	c.One(2, 2, false)
	c.One(3, 2, false)

	c.Connect((leader + 1) % 3)
	c.One(4, 3, true)
	c.CheckNoErrors()
}

func TestFailNoAgree(t *testing.T) {
	c := test.MakeCluster(t, 5, false)
	defer c.Shutdown()
	leader := c.CheckOneLeader()

	for i := 0; i < 5; i++ {
		if i != leader {
			c.Disconnect(i)
		}
	}
	c.Disconnect(leader)
	time.Sleep(200 * time.Millisecond)
	nd, _ := c.NCommitted(1 + int(0))
	// A minority (just the old leader) cannot commit anything new.
	if nd > 0 {
		t.Fatalf("expected no commit without a majority, got %d", nd)
	}

	for i := 0; i < 5; i++ {
		c.Connect(i)
	}
	c.CheckOneLeader()
	c.One(42, 5, true)
}

// ---- Phase 3: persistence ----

func TestPersist1(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()

	c.One(11, 3, true)
	leader := c.CheckOneLeader()
	c.Crash1(leader)
	c.Restart1(leader)
	c.CheckOneLeader()
	c.One(12, 3, true)
}

func TestPersist2(t *testing.T) {
	c := test.MakeCluster(t, 3, false)
	defer c.Shutdown()

	c.One(101, 3, true)
	for i := 0; i < 3; i++ {
		c.Crash1(i)
		c.Restart1(i)
		c.CheckOneLeader()
	}
	c.One(102, 3, true)
	c.CheckNoErrors()
}

func TestFigure8Unreliable(t *testing.T) {
	c := test.MakeCluster(t, 5, true)
	defer c.Shutdown()
	c.SetLongDelays(true)

	index := 1
	for iters := 0; iters < 30; iters++ {
		leaderIdx := -1
		for i := range c.Rafts() {
			if _, isLeader := c.Rafts()[i].GetState(); isLeader {
				leaderIdx = i
			}
		}
		if leaderIdx >= 0 {
			c.Rafts()[leaderIdx].Start(index * 10)
			index++
		}
		if (iters%3) == 1 && leaderIdx >= 0 {
			c.Crash1(leaderIdx)
			time.Sleep(20 * time.Millisecond)
			c.Restart1(leaderIdx)
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.SetUnreliable(false)
	c.SetLongDelays(false)
	time.Sleep(500 * time.Millisecond)
	c.CheckNoErrors()
}
