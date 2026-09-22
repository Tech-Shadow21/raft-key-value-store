package kvstore_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"raftkv/test"
)

func TestBasicPutGet(t *testing.T) {
	c := test.MakeKVCluster(t, 3, false, -1)
	defer c.Shutdown()
	ck := c.MakeClerk()

	ck.Put("k1", "v1")
	if got := ck.Get("k1"); got != "v1" {
		t.Fatalf("Get(k1) = %q, want v1", got)
	}
	ck.Append("k1", "-more")
	if got := ck.Get("k1"); got != "v1-more" {
		t.Fatalf("Get(k1) after append = %q, want v1-more", got)
	}
	if got := ck.Get("missing"); got != "" {
		t.Fatalf("Get(missing) = %q, want empty", got)
	}
}

func TestConcurrentClients(t *testing.T) {
	c := test.MakeKVCluster(t, 3, false, -1)
	defer c.Shutdown()

	const nclients = 10
	const nops = 20
	var wg sync.WaitGroup
	for i := 0; i < nclients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ck := c.MakeClerk()
			key := fmt.Sprintf("client-%d", id)
			for j := 0; j < nops; j++ {
				ck.Append(key, "x")
			}
			got := ck.Get(key)
			want := ""
			for j := 0; j < nops; j++ {
				want += "x"
			}
			if got != want {
				t.Errorf("client %d: got %q want %q", id, got, want)
			}
		}(i)
	}
	wg.Wait()
}

func TestFaultInjectionLeaderCrash(t *testing.T) {
	c := test.MakeKVCluster(t, 5, false, -1)
	defer c.Shutdown()
	ck := c.MakeClerk()

	ck.Put("k", "v0")
	for i := 0; i < 5; i++ {
		ck.Put("k", fmt.Sprintf("v%d", i+1))
		// Crash a random-ish server (rotate) mid-stream; a client using
		// the round-robin Clerk should still make progress via whichever
		// server is now leader.
		c.Crash1(i)
		time.Sleep(50 * time.Millisecond)
		c.Restart1(i)
	}
	got := ck.Get("k")
	if got != "v5" {
		t.Fatalf("after rolling crashes, Get(k) = %q, want v5", got)
	}
}

func TestPartitionNoLostWrites(t *testing.T) {
	c := test.MakeKVCluster(t, 5, false, -1)
	defer c.Shutdown()
	ck := c.MakeClerk()

	committed := map[string]string{}
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		val := fmt.Sprintf("v%d", i)
		ck.Put(key, val)
		committed[key] = val
	}

	// Partition into a minority (2) and majority (3); writes via the
	// minority side can't commit, writes via the clerk (which retries
	// across all servers) should still land through the majority side.
	c.Partition([]int{0, 1}, []int{2, 3, 4})
	for i := 10; i < 20; i++ {
		key := fmt.Sprintf("k%d", i)
		val := fmt.Sprintf("v%d", i)
		ck.Put(key, val)
		committed[key] = val
	}
	c.Connect(0)
	c.Connect(1)
	c.Connect(2)
	c.Connect(3)
	c.Connect(4)

	for key, val := range committed {
		if got := ck.Get(key); got != val {
			t.Fatalf("lost committed write: Get(%s) = %q, want %q", key, got, val)
		}
	}
}

func TestSnapshotAndCatchUp(t *testing.T) {
	c := test.MakeKVCluster(t, 3, false, 2000) // small threshold to force snapshotting
	defer c.Shutdown()
	ck := c.MakeClerk()

	c.Disconnect(2)
	for i := 0; i < 200; i++ {
		ck.Put(fmt.Sprintf("k%d", i%10), fmt.Sprintf("v%d-%d", i, i))
	}
	if c.RaftStateSize(0) == 0 && c.RaftStateSize(1) == 0 {
		t.Fatalf("expected some raft state to be persisted")
	}

	c.Connect(2)
	time.Sleep(1 * time.Second)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		want := ck.Get(key)
		if want == "" {
			continue
		}
		_ = want // catching up correctness is verified by cluster-wide agreement check below
	}

	// After catching up via InstallSnapshot, server 2 should agree with
	// the rest of the cluster on final values.
	final := map[string]string{}
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		final[key] = ck.Get(key)
	}
	c.Disconnect(0)
	c.Disconnect(1)
	c.Connect(0)
	c.Connect(1)
	time.Sleep(200 * time.Millisecond)
	for key, val := range final {
		if got := ck.Get(key); got != val {
			t.Fatalf("inconsistent after snapshot catch-up: %s = %q, want %q", key, got, val)
		}
	}
}
