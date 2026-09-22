package raft

import (
	"os"
	"path/filepath"
	"sync"
)

// Persister stores the durable Raft state (currentTerm, votedFor, log) and,
// separately, a snapshot of the replicated state machine. Implementations
// must make Save durable (e.g. fsync) before returning, since callers rely
// on it having survived a crash immediately after.
type Persister interface {
	SaveRaftState(state []byte)
	ReadRaftState() []byte
	SaveStateAndSnapshot(state []byte, snapshot []byte)
	ReadSnapshot() []byte
	RaftStateSize() int
}

// MemoryPersister keeps state in memory only. Used by tests to simulate a
// process crash+restart without actually touching disk: the test harness
// holds a reference to the same Persister across a Crash1/Restart1 pair.
type MemoryPersister struct {
	mu        sync.Mutex
	raftstate []byte
	snapshot  []byte
}

func MakeMemoryPersister() *MemoryPersister {
	return &MemoryPersister{}
}

func (p *MemoryPersister) Copy() *MemoryPersister {
	p.mu.Lock()
	defer p.mu.Unlock()
	np := MakeMemoryPersister()
	np.raftstate = append([]byte{}, p.raftstate...)
	np.snapshot = append([]byte{}, p.snapshot...)
	return np
}

func (p *MemoryPersister) SaveRaftState(state []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raftstate = append([]byte{}, state...)
}

func (p *MemoryPersister) ReadRaftState() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte{}, p.raftstate...)
}

func (p *MemoryPersister) SaveStateAndSnapshot(state []byte, snapshot []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raftstate = append([]byte{}, state...)
	p.snapshot = append([]byte{}, snapshot...)
}

func (p *MemoryPersister) ReadSnapshot() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte{}, p.snapshot...)
}

func (p *MemoryPersister) RaftStateSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.raftstate)
}

// FilePersister writes state to disk, fsyncing before returning so an
// acknowledged RPC's effect can't be lost to a crash. Production use.
type FilePersister struct {
	mu        sync.Mutex
	dir       string
	statePath string
	snapPath  string
}

func MakeFilePersister(dir string) (*FilePersister, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &FilePersister{
		dir:       dir,
		statePath: filepath.Join(dir, "raftstate.bin"),
		snapPath:  filepath.Join(dir, "snapshot.bin"),
	}, nil
}

func writeFileSync(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readFileOrEmpty(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

func (p *FilePersister) SaveRaftState(state []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = writeFileSync(p.statePath, state)
}

func (p *FilePersister) ReadRaftState() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return readFileOrEmpty(p.statePath)
}

func (p *FilePersister) SaveStateAndSnapshot(state []byte, snapshot []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = writeFileSync(p.statePath, state)
	_ = writeFileSync(p.snapPath, snapshot)
}

func (p *FilePersister) ReadSnapshot() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return readFileOrEmpty(p.snapPath)
}

func (p *FilePersister) RaftStateSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(readFileOrEmpty(p.statePath))
}
