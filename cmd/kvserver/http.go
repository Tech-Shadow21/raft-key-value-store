package main

import (
	"encoding/json"
	"log"
	"net/http"

	"raftkv/kvstore"
)

// httpAPI exposes a small JSON API for the web dashboard (web/), separate
// from the Raft/KVServer RPC port. It's intentionally thin: status reads
// straight from this node's local Raft state (no consensus round needed —
// that's exactly the kind of query a dashboard wants even from a
// follower), while reads/writes go through a Clerk so they succeed
// regardless of which node's HTTP port the dashboard happens to be
// polling.
type httpAPI struct {
	id    int
	peers map[int]string
	node  *node
	ck    *kvstore.Clerk
	demo  bool
}

type statusResponse struct {
	Id     int            `json:"id"`
	Term   int            `json:"term"`
	Leader bool           `json:"leader"`
	Peers  map[int]string `json:"peers"`
	// Powered is false while the node is switched off from the dashboard;
	// DemoControls says whether the dashboard may switch it at all.
	Powered      bool `json:"powered"`
	DemoControls bool `json:"demoControls"`
}

type kvGetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type kvWriteRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Op    string `json:"op"` // "put" or "append"
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func (a *httpAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{Id: a.id, Peers: a.peers, DemoControls: a.demo}
	if kv := a.node.current(); kv != nil {
		resp.Term, resp.Leader = kv.Raft().GetState()
		resp.Powered = true
	}
	writeJSON(w, resp)
}

type powerRequest struct {
	On bool `json:"on"`
}

func (a *httpAPI) handlePower(w http.ResponseWriter, r *http.Request) {
	if !a.demo {
		http.Error(w, "demo controls disabled (start kvserver with -demo-controls)", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req powerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	a.node.setPower(req.On)
	log.Printf("demo control: powered %s", map[bool]string{true: "on", false: "off"}[req.On])
	writeJSON(w, map[string]bool{"powered": req.On})
}

func (a *httpAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	value := a.ck.Get(key)
	writeJSON(w, kvGetResponse{Key: key, Value: value, Found: value != ""})
}

func (a *httpAPI) handleWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req kvWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	switch req.Op {
	case "append":
		a.ck.Append(req.Key, req.Value)
	default:
		a.ck.Put(req.Key, req.Value)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// startHTTPServer registers the dashboard API on addr and serves it in the
// background. peerAddrs is id->addr for every node (including this one),
// used both to report in /api/status and to build the Clerk that fans
// writes/reads out across the whole cluster.
func startHTTPServer(addr string, id int, peerAddrs map[int]string, n *node, ck *kvstore.Clerk, demo bool) {
	a := &httpAPI{id: id, peers: peerAddrs, node: n, ck: ck, demo: demo}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", withCORS(a.handleStatus))
	mux.HandleFunc("/api/power", withCORS(a.handlePower))
	mux.HandleFunc("/api/kv", withCORS(func(w http.ResponseWriter, r *http.Request) {
		// A switched-off node shouldn't serve anything, even though its
		// Clerk could still reach the rest of the cluster.
		if a.node.current() == nil {
			http.Error(w, errPoweredOff.Error(), http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodGet {
			a.handleGet(w, r)
			return
		}
		a.handleWrite(w, r)
	}))
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			panic(err)
		}
	}()
}
