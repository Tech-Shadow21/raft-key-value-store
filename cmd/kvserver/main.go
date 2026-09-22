// Command kvserver runs one node of a raft-kv-store cluster as a real
// process communicating over TCP, per docs/DESIGN.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"raftkv/kvstore"
	"raftkv/raft"
	"raftkv/transport"
)

func main() {
	id := flag.Int("id", -1, "this node's id (must be a key in -peers)")
	peersFlag := flag.String("peers", "", "comma-separated id=host:port list, e.g. 1=localhost:9001,2=localhost:9002,3=localhost:9003")
	dataDir := flag.String("data", "", "directory for persistent Raft/KV state")
	maxRaftState := flag.Int("max-raft-state", -1, "snapshot once persisted Raft state exceeds this many bytes (-1 disables)")
	verbose := flag.Bool("verbose", true, "log leader/term changes and commit latency")
	httpAddr := flag.String("http", "", "address for the dashboard JSON API, e.g. localhost:8001 (empty disables it)")
	flag.Parse()

	if *id < 0 || *peersFlag == "" || *dataDir == "" {
		fmt.Fprintln(os.Stderr, "usage: kvserver -id <id> -peers <id=host:port,...> -data <dir>")
		os.Exit(2)
	}

	peers, err := parsePeers(*peersFlag)
	if err != nil {
		log.Fatalf("parsing -peers: %v", err)
	}
	self, ok := peers[*id]
	if !ok {
		log.Fatalf("id %d not present in -peers", *id)
	}

	persister, err := raft.MakeFilePersister(*dataDir)
	if err != nil {
		log.Fatalf("opening data dir %s: %v", *dataDir, err)
	}

	ids := make([]int, 0, len(peers))
	for pid := range peers {
		ids = append(ids, pid)
	}
	sort.Ints(ids)

	ends := make([]transport.ClientEnd, len(ids))
	idIndex := map[int]int{}
	for i, pid := range ids {
		idIndex[pid] = i
		if pid == *id {
			ends[i] = nil // unused self-end
			continue
		}
		ends[i] = transport.NewTCPEnd(peers[pid])
	}

	tcpSrv, err := transport.NewTCPServer(self)
	if err != nil {
		log.Fatalf("listening on %s: %v", self, err)
	}
	log.Printf("kvserver id=%d listening on %s, peers=%v", *id, self, peers)

	kv := kvstore.StartServer(ends, idIndex[*id], persister, *maxRaftState)
	kv.Raft().Verbose = *verbose
	tcpSrv.RegisterName("KVServer", kv)
	tcpSrv.RegisterName("Raft", kv.Raft())

	go statusLoop(kv)

	if *httpAddr != "" {
		clerkEnds := make([]transport.ClientEnd, len(ids))
		for i, pid := range ids {
			clerkEnds[i] = transport.NewTCPEnd(peers[pid])
		}
		ck := kvstore.MakeClerk(clerkEnds)
		startHTTPServer(*httpAddr, *id, peers, kv, ck)
		log.Printf("dashboard API listening on %s", *httpAddr)
	}

	select {}
}

func statusLoop(kv *kvstore.Server) {
	for {
		time.Sleep(5 * time.Second)
		term, isLeader := kv.Raft().GetState()
		log.Printf("status: term=%d leader=%v", term, isLeader)
	}
}

func parsePeers(s string) (map[int]string, error) {
	out := map[int]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("bad peer entry %q, want id=host:port", part)
		}
		id, err := strconv.Atoi(kv[0])
		if err != nil {
			return nil, fmt.Errorf("bad peer id %q: %w", kv[0], err)
		}
		out[id] = kv[1]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no peers given")
	}
	return out, nil
}
