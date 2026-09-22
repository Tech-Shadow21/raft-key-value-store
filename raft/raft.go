// Package raft implements the Raft consensus algorithm (Ongaro & Ousterhout,
// "In Search of an Understandable Consensus Algorithm"). A Raft instance
// exposes Start(command) to append to the replicated log and delivers
// committed entries on ApplyCh; it knows nothing about what commands mean —
// that's kvstore's job.
package raft

import (
	"bytes"
	"encoding/gob"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"raftkv/transport"
)

type State int

const (
	Follower State = iota
	Candidate
	Leader
)

func (s State) String() string {
	switch s {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	default:
		return "unknown"
	}
}

// ApplyMsg is sent on ApplyCh once a command (or a snapshot) is safe to
// apply to the state machine. Exactly one of CommandValid / SnapshotValid
// is true per message.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
	CommandTerm  int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type Raft struct {
	mu        sync.Mutex
	peers     []transport.ClientEnd
	persister Persister
	me        int
	dead      int32

	state       State
	currentTerm int
	votedFor    int
	log         []LogEntry // log[0] is a sentinel: {Term: lastIncludedTerm, Index: lastIncludedIndex}

	commitIndex int
	lastApplied int

	nextIndex  []int
	matchIndex []int

	electionDeadline time.Time
	applyCh          chan ApplyMsg
	applyCond        *sync.Cond

	// startTimes records when Start() proposed each still-uncommitted log
	// index, purely so we can log commit latency once it's applied. Purged
	// as entries commit or are overwritten.
	startTimes map[int]time.Time

	// Verbose enables per-event logging (leader/term changes, commit
	// latency). Off by default so test output stays quiet; cmd/kvserver
	// turns it on.
	Verbose bool
	Logger  *log.Logger
}

func (rf *Raft) logf(format string, args ...interface{}) {
	if rf.Verbose && rf.Logger != nil {
		rf.Logger.Printf("[raft %d] "+format, append([]interface{}{rf.me}, args...)...)
	}
}

// Make creates a Raft peer. peers[me] is this node's own (unused) end;
// applyCh receives committed entries and snapshots. Make starts the
// election-timeout and log-applier goroutines and returns immediately.
func Make(peers []transport.ClientEnd, me int, persister Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		peers:      peers,
		persister:  persister,
		me:         me,
		state:      Follower,
		votedFor:   -1,
		log:        []LogEntry{{Term: 0, Index: 0}},
		applyCh:    applyCh,
		startTimes: map[int]time.Time{},
		Logger:     log.New(log.Writer(), "", log.LstdFlags),
	}
	rf.applyCond = sync.NewCond(&rf.mu)
	rf.readPersist(persister.ReadRaftState())
	rf.resetElectionDeadline()

	go rf.electionTicker()
	go rf.applier()

	return rf
}

func (rf *Raft) killed() bool {
	return atomic.LoadInt32(&rf.dead) == 1
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	rf.mu.Lock()
	rf.applyCond.Broadcast()
	rf.mu.Unlock()
}

func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
}

// ---- log index helpers (log[0] holds lastIncludedIndex/Term) ----

func (rf *Raft) lastLogIndex() int { return rf.log[len(rf.log)-1].Index }
func (rf *Raft) lastLogTerm() int  { return rf.log[len(rf.log)-1].Term }

// toSliceIndex converts an absolute log index to an index into rf.log.
// Returns -1 if the entry has been compacted away.
func (rf *Raft) toSliceIndex(absIndex int) int {
	base := rf.log[0].Index
	if absIndex < base {
		return -1
	}
	i := absIndex - base
	if i >= len(rf.log) {
		return -1
	}
	return i
}

func (rf *Raft) entryAt(absIndex int) LogEntry {
	return rf.log[rf.toSliceIndex(absIndex)]
}

// ---- persistence ----

func (rf *Raft) persist() {
	buf := new(bytes.Buffer)
	e := gob.NewEncoder(buf)
	_ = e.Encode(rf.currentTerm)
	_ = e.Encode(rf.votedFor)
	_ = e.Encode(rf.log)
	rf.persister.SaveRaftState(buf.Bytes())
}

func (rf *Raft) persistWithSnapshot(snapshot []byte) {
	buf := new(bytes.Buffer)
	e := gob.NewEncoder(buf)
	_ = e.Encode(rf.currentTerm)
	_ = e.Encode(rf.votedFor)
	_ = e.Encode(rf.log)
	rf.persister.SaveStateAndSnapshot(buf.Bytes(), snapshot)
}

func (rf *Raft) readPersist(data []byte) {
	if len(data) == 0 {
		return
	}
	r := bytes.NewReader(data)
	d := gob.NewDecoder(r)
	var currentTerm, votedFor int
	var logEntries []LogEntry
	if err := d.Decode(&currentTerm); err != nil {
		return
	}
	if err := d.Decode(&votedFor); err != nil {
		return
	}
	if err := d.Decode(&logEntries); err != nil {
		return
	}
	rf.currentTerm = currentTerm
	rf.votedFor = votedFor
	rf.log = logEntries
	rf.lastApplied = rf.log[0].Index
	rf.commitIndex = rf.log[0].Index
}

// ---- snapshotting ----

// Snapshot is called by the service (kvstore) once it has persisted its own
// state machine up through index; Raft discards log entries <= index.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	base := rf.log[0].Index
	if index <= base || index > rf.lastLogIndex() {
		return
	}
	newLog := []LogEntry{{Term: rf.entryAt(index).Term, Index: index}}
	newLog = append(newLog, rf.log[rf.toSliceIndex(index)+1:]...)
	rf.log = newLog
	rf.persistWithSnapshot(snapshot)
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	if args.Term < rf.currentTerm {
		return nil
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}
	rf.resetElectionDeadline()

	if args.LastIncludedIndex <= rf.log[0].Index {
		return nil
	}

	if args.LastIncludedIndex <= rf.lastLogIndex() && rf.entryAt(args.LastIncludedIndex).Term == args.LastIncludedTerm {
		newLog := []LogEntry{{Term: args.LastIncludedTerm, Index: args.LastIncludedIndex}}
		newLog = append(newLog, rf.log[rf.toSliceIndex(args.LastIncludedIndex)+1:]...)
		rf.log = newLog
	} else {
		rf.log = []LogEntry{{Term: args.LastIncludedTerm, Index: args.LastIncludedIndex}}
	}

	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	rf.persistWithSnapshot(args.Data)

	if rf.lastApplied < args.LastIncludedIndex {
		rf.lastApplied = args.LastIncludedIndex
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,
			Snapshot:      args.Data,
			SnapshotTerm:  args.LastIncludedTerm,
			SnapshotIndex: args.LastIncludedIndex,
		}
	}
	return nil
}

func (rf *Raft) sendInstallSnapshot(peer int) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	args := &InstallSnapshotArgs{
		Term:              rf.currentTerm,
		LeaderId:          rf.me,
		LastIncludedIndex: rf.log[0].Index,
		LastIncludedTerm:  rf.log[0].Term,
		Data:              rf.persister.ReadSnapshot(),
	}
	rf.mu.Unlock()

	var reply InstallSnapshotReply
	if !rf.peers[peer].Call("Raft.InstallSnapshot", args, &reply) {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		return
	}
	if rf.state != Leader || rf.currentTerm != args.Term {
		return
	}
	if args.LastIncludedIndex+1 > rf.nextIndex[peer] {
		rf.nextIndex[peer] = args.LastIncludedIndex + 1
	}
	if args.LastIncludedIndex > rf.matchIndex[peer] {
		rf.matchIndex[peer] = args.LastIncludedIndex
	}
}

// ---- state transitions ----

// becomeFollower must be called with rf.mu held.
func (rf *Raft) becomeFollower(term int) {
	wasLeader := rf.state == Leader
	rf.state = Follower
	rf.currentTerm = term
	rf.votedFor = -1
	rf.persist()
	if wasLeader {
		rf.logf("stepping down from leader, new term %d", term)
	}
}

func (rf *Raft) resetElectionDeadline() {
	timeout := ElectionTimeoutMin + time.Duration(rand.Int63n(int64(ElectionTimeoutRange)))
	rf.electionDeadline = time.Now().Add(timeout)
}

// ---- RequestVote ----

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	if args.Term < rf.currentTerm {
		return nil
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
		reply.Term = rf.currentTerm
	}

	upToDate := args.LastLogTerm > rf.lastLogTerm() ||
		(args.LastLogTerm == rf.lastLogTerm() && args.LastLogIndex >= rf.lastLogIndex())

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && upToDate {
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true
		rf.persist()
		rf.resetElectionDeadline()
	}
	return nil
}

func (rf *Raft) sendRequestVote(peer int, args *RequestVoteArgs, votes *int32, term int) {
	var reply RequestVoteReply
	if !rf.peers[peer].Call("Raft.RequestVote", args, &reply) {
		return
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		return
	}
	if rf.state != Candidate || rf.currentTerm != term {
		return
	}
	if reply.VoteGranted {
		n := atomic.AddInt32(votes, 1)
		if int(n) > len(rf.peers)/2 && rf.state == Candidate {
			rf.becomeLeader()
		}
	}
}

// becomeLeader must be called with rf.mu held.
func (rf *Raft) becomeLeader() {
	rf.state = Leader
	n := len(rf.peers)
	rf.nextIndex = make([]int, n)
	rf.matchIndex = make([]int, n)
	for i := range rf.peers {
		rf.nextIndex[i] = rf.lastLogIndex() + 1
		rf.matchIndex[i] = 0
	}
	rf.logf("elected leader for term %d (last log index %d)", rf.currentTerm, rf.lastLogIndex())
	go rf.leaderHeartbeatLoop(rf.currentTerm)
}

func (rf *Raft) startElection() {
	rf.mu.Lock()
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persist()
	term := rf.currentTerm
	args := &RequestVoteArgs{
		Term:         term,
		CandidateId:  rf.me,
		LastLogIndex: rf.lastLogIndex(),
		LastLogTerm:  rf.lastLogTerm(),
	}
	rf.resetElectionDeadline()
	rf.mu.Unlock()

	var votes int32 = 1 // vote for self
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go rf.sendRequestVote(i, args, &votes, term)
	}
}

func (rf *Raft) electionTicker() {
	for !rf.killed() {
		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		state := rf.state
		expired := time.Now().After(rf.electionDeadline)
		rf.mu.Unlock()
		if state != Leader && expired {
			rf.startElection()
		}
	}
}

// ---- AppendEntries ----

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.Success = false

	if args.Term < rf.currentTerm {
		return nil
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
		reply.Term = rf.currentTerm
	} else if rf.state == Candidate {
		rf.state = Follower
	}
	rf.resetElectionDeadline()

	base := rf.log[0].Index
	if args.PrevLogIndex < base {
		// Prefix already compacted into our snapshot; treat as matching
		// and let leader's future RPCs (or InstallSnapshot) reconcile it.
		reply.Success = true
		reply.ConflictIndex = base + 1
		return nil
	}
	if args.PrevLogIndex > rf.lastLogIndex() {
		reply.ConflictIndex = rf.lastLogIndex() + 1
		reply.ConflictTerm = 0
		return nil
	}
	if rf.entryAt(args.PrevLogIndex).Term != args.PrevLogTerm {
		conflictTerm := rf.entryAt(args.PrevLogIndex).Term
		reply.ConflictTerm = conflictTerm
		i := args.PrevLogIndex
		for i > base && rf.entryAt(i).Term == conflictTerm {
			i--
		}
		reply.ConflictIndex = i + 1
		return nil
	}

	// Find first conflicting entry and truncate/append from there.
	insertAt := args.PrevLogIndex + 1
	for i, e := range args.Entries {
		idx := insertAt + i
		if idx > rf.lastLogIndex() {
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
		if rf.entryAt(idx).Term != e.Term {
			rf.log = rf.log[:rf.toSliceIndex(idx)]
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
	}
	rf.persist()

	if args.LeaderCommit > rf.commitIndex {
		newCommit := args.LeaderCommit
		if rf.lastLogIndex() < newCommit {
			newCommit = rf.lastLogIndex()
		}
		rf.commitIndex = newCommit
		rf.applyCond.Broadcast()
	}

	reply.Success = true
	return nil
}

func (rf *Raft) sendAppendEntries(peer int, term int) {
	rf.mu.Lock()
	if rf.state != Leader || rf.currentTerm != term {
		rf.mu.Unlock()
		return
	}
	base := rf.log[0].Index
	if rf.nextIndex[peer] <= base {
		rf.mu.Unlock()
		rf.sendInstallSnapshot(peer)
		return
	}
	prevLogIndex := rf.nextIndex[peer] - 1
	prevLogTerm := rf.entryAt(prevLogIndex).Term
	var entries []LogEntry
	if rf.nextIndex[peer] <= rf.lastLogIndex() {
		src := rf.log[rf.toSliceIndex(rf.nextIndex[peer]):]
		entries = append(entries, src...)
	}
	args := &AppendEntriesArgs{
		Term:         term,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.Unlock()

	var reply AppendEntriesReply
	if !rf.peers[peer].Call("Raft.AppendEntries", args, &reply) {
		return
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if reply.Term > rf.currentTerm {
		rf.becomeFollower(reply.Term)
		return
	}
	if rf.state != Leader || rf.currentTerm != term {
		return
	}

	if reply.Success {
		newMatch := args.PrevLogIndex + len(args.Entries)
		if newMatch > rf.matchIndex[peer] {
			rf.matchIndex[peer] = newMatch
		}
		rf.nextIndex[peer] = rf.matchIndex[peer] + 1
		rf.advanceCommitIndex()
		return
	}

	// Fast backtrack using conflict hints.
	if reply.ConflictTerm != 0 {
		lastIdxOfTerm := -1
		for i := rf.lastLogIndex(); i > rf.log[0].Index; i-- {
			if rf.entryAt(i).Term == reply.ConflictTerm {
				lastIdxOfTerm = i
				break
			}
		}
		if lastIdxOfTerm >= 0 {
			rf.nextIndex[peer] = lastIdxOfTerm + 1
		} else {
			rf.nextIndex[peer] = reply.ConflictIndex
		}
	} else {
		rf.nextIndex[peer] = reply.ConflictIndex
	}
	if rf.nextIndex[peer] < 1 {
		rf.nextIndex[peer] = 1
	}
}

// advanceCommitIndex must be called with rf.mu held. Advances commitIndex
// to the highest N for which a majority of matchIndex[i] >= N and
// log[N].Term == currentTerm (Raft §5.4.2 — never commit an entry from a
// prior term purely by counting replicas).
func (rf *Raft) advanceCommitIndex() {
	n := len(rf.peers)
	for N := rf.lastLogIndex(); N > rf.commitIndex; N-- {
		if N <= rf.log[0].Index {
			break
		}
		if rf.entryAt(N).Term != rf.currentTerm {
			continue
		}
		count := 1
		for i := 0; i < n; i++ {
			if i != rf.me && rf.matchIndex[i] >= N {
				count++
			}
		}
		if count > n/2 {
			old := rf.commitIndex
			rf.commitIndex = N
			if rf.Verbose {
				for idx := old + 1; idx <= N; idx++ {
					if t0, ok := rf.startTimes[idx]; ok {
						rf.logf("committed index %d (term %d) after %s", idx, rf.currentTerm, time.Since(t0))
						delete(rf.startTimes, idx)
					}
				}
			}
			rf.applyCond.Broadcast()
			break
		}
	}
}

func (rf *Raft) leaderHeartbeatLoop(term int) {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.state != Leader || rf.currentTerm != term {
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()

		for i := range rf.peers {
			if i == rf.me {
				continue
			}
			go rf.sendAppendEntries(i, term)
		}
		time.Sleep(HeartbeatInterval)
	}
}

// ---- client-facing API ----

// Start appends command to the log if this peer is the leader and returns
// the index it will occupy, the current term, and true. It does not block
// for the entry to commit.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != Leader {
		return -1, rf.currentTerm, false
	}
	index := rf.lastLogIndex() + 1
	entry := LogEntry{Term: rf.currentTerm, Index: index, Command: command}
	rf.log = append(rf.log, entry)
	rf.persist()
	if rf.Verbose {
		rf.startTimes[index] = time.Now()
	}

	rf.matchIndex[rf.me] = index
	rf.nextIndex[rf.me] = index + 1

	term := rf.currentTerm
	go func() {
		for i := range rf.peers {
			if i != rf.me {
				go rf.sendAppendEntries(i, term)
			}
		}
	}()

	return index, term, true
}

// applier delivers committed entries (and installed snapshots) to applyCh
// in order, one at a time, as commitIndex advances.
func (rf *Raft) applier() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	for {
		for rf.lastApplied >= rf.commitIndex && !rf.killed() {
			rf.applyCond.Wait()
		}
		if rf.killed() {
			return
		}
		rf.lastApplied++
		idx := rf.lastApplied
		if idx <= rf.log[0].Index {
			// Compacted away concurrently; skip forward to the snapshot.
			continue
		}
		entry := rf.entryAt(idx)
		msg := ApplyMsg{
			CommandValid: true,
			Command:      entry.Command,
			CommandIndex: entry.Index,
			CommandTerm:  entry.Term,
		}
		rf.mu.Unlock()
		rf.applyCh <- msg
		rf.mu.Lock()
	}
}
