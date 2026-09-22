package main

import (
	"encoding/json"
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
	kv    *kvstore.Server
	ck    *kvstore.Clerk
}

type statusResponse struct {
	Id     int            `json:"id"`
	Term   int            `json:"term"`
	Leader bool           `json:"leader"`
	Peers  map[int]string `json:"peers"`
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
	term, isLeader := a.kv.Raft().GetState()
	writeJSON(w, statusResponse{
		Id:     a.id,
		Term:   term,
		Leader: isLeader,
		Peers:  a.peers,
	})
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
func startHTTPServer(addr string, id int, peerAddrs map[int]string, kv *kvstore.Server, ck *kvstore.Clerk) {
	a := &httpAPI{id: id, peers: peerAddrs, kv: kv, ck: ck}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", withCORS(a.handleStatus))
	mux.HandleFunc("/api/kv", withCORS(func(w http.ResponseWriter, r *http.Request) {
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
