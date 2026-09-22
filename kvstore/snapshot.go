package kvstore

import (
	"bytes"
	"encoding/gob"
)

// encodeSnapshot must be called with kv.mu held.
func (kv *Server) encodeSnapshot() []byte {
	buf := new(bytes.Buffer)
	e := gob.NewEncoder(buf)
	_ = e.Encode(kv.data)
	_ = e.Encode(kv.lastSeq)
	return buf.Bytes()
}

// restoreSnapshot must be called with kv.mu held (or before any concurrent
// access begins, e.g. during StartServer).
func (kv *Server) restoreSnapshot(data []byte) {
	if len(data) == 0 {
		return
	}
	r := bytes.NewReader(data)
	d := gob.NewDecoder(r)
	var m map[string]string
	var seq map[int64]int64
	if err := d.Decode(&m); err != nil {
		return
	}
	if err := d.Decode(&seq); err != nil {
		return
	}
	kv.data = m
	kv.lastSeq = seq
}

func (kv *Server) Kill() {
	kv.rf.Kill()
}
