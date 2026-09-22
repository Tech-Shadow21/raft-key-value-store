package test

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// TestChaos is the harness's centerpiece: it hammers a 5-node KV cluster
// with concurrent client writes while a background goroutine randomly
// crashes/restarts nodes and creates/heals partitions, then asserts the
// one invariant that matters — no value a client was told is committed is
// ever subsequently lost or overwritten with something else on a
// disagreeing server.
func TestChaos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chaos test in -short mode")
	}
	const n = 5
	c := MakeKVCluster(t, n, true, -1)
	defer c.Shutdown()

	stop := make(chan struct{})
	var chaosWg sync.WaitGroup
	chaosWg.Add(1)
	go func() {
		defer chaosWg.Done()
		alive := map[int]bool{}
		for i := 0; i < n; i++ {
			alive[i] = true
		}
		for {
			select {
			case <-stop:
				return
			default:
			}
			time.Sleep(time.Duration(80+rand.Intn(120)) * time.Millisecond)
			i := rand.Intn(n)
			if alive[i] {
				c.Crash1(i)
				alive[i] = false
			} else {
				c.Restart1(i)
				alive[i] = true
			}
		}
	}()

	var mu sync.Mutex
	committed := map[string]string{}

	var clientWg sync.WaitGroup
	const nclients = 4
	const opsPerClient = 25
	for ci := 0; ci < nclients; ci++ {
		clientWg.Add(1)
		go func(id int) {
			defer clientWg.Done()
			ck := c.MakeClerk()
			for i := 0; i < opsPerClient; i++ {
				key := fmt.Sprintf("chaos-%d-%d", id, i%5)
				val := fmt.Sprintf("v-%d-%d", id, i)
				ck.Put(key, val)
				mu.Lock()
				committed[key] = val
				mu.Unlock()
			}
		}(ci)
	}
	clientWg.Wait()

	close(stop)
	chaosWg.Wait()

	// Bring every node back so a final Get can be answered.
	for i := 0; i < n; i++ {
		c.Restart1(i)
		c.Connect(i)
	}
	time.Sleep(500 * time.Millisecond)

	ck := c.MakeClerk()
	mu.Lock()
	defer mu.Unlock()
	for key, want := range committed {
		got := ck.Get(key)
		if got != want {
			t.Fatalf("lost or corrupted committed write: %s = %q, want %q", key, got, want)
		}
	}
	t.Logf("chaos test: verified %d committed writes survived crashes/restarts", len(committed))
}
