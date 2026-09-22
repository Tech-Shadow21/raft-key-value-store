// Package transport provides the RPC fabric raft.Raft and kvstore.Server run
// over. It has two implementations behind the same client interface:
//
//   - Network (this file): fully in-memory, used by tests. Lets a test
//     control exactly which RPCs are delivered, dropped, delayed, or
//     blocked by a simulated partition, and simulates process crash/restart
//     via GetServer/Enable without actually starting new OS processes.
//   - TCP (tcp.go): real sockets, used by cmd/kvserver for an actual
//     multi-process/multi-host cluster.
//
// The design follows MIT 6.5840's labrpc package (reflection-dispatched
// RPC over Go channels) because that pattern is exactly what's needed to
// build a deterministic-enough fault-injection harness: real concurrency
// and real serialization-shaped semantics (arguments are gob-encoded, so a
// handler can't accidentally share memory with the caller) without needing
// real sockets or real timeouts in unit tests.
package transport

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"math/rand"
	"reflect"
	"sync"
	"time"
)

// ClientEnd is what a raft/kvstore instance holds one of per peer. Call()
// blocks until it gets a reply or decides the RPC failed (peer down,
// disconnected, or — in the real transport — a network/timeout error).
type ClientEnd interface {
	Call(svcMeth string, args interface{}, reply interface{}) bool
	// Name returns a human-readable identifier for the peer (e.g. "3" or
	// "localhost:9003"), used for logging.
	Name() string
}

// Server exposes RegisterName so raft.Raft and kvstore.Server can install
// their RPC handlers without transport needing to import those packages.
type Server interface {
	// RegisterName registers rcvr's exported methods (each must have the
	// signature func(argsPtr, replyPtr) — no error return, matching Go's
	// net/rpc convention) under svcName, so callers dial "svcName.Method".
	RegisterName(svcName string, rcvr interface{})
}

// ---- in-memory implementation ----

type reqMsg struct {
	endname  interface{}
	svcMeth  string
	argsType reflect.Type
	args     []byte
	replyCh  chan replyMsg
}

type replyMsg struct {
	ok    bool
	reply []byte
}

type Network struct {
	mu          sync.Mutex
	reliable    bool
	longDelays  bool // pause a long time on send on disabled connection
	ended       map[interface{}]bool
	servers     map[interface{}]*server
	connections map[interface{}]interface{} // endname -> server name
	endCh       chan reqMsg
	done        chan struct{}
	bytesSent   int64
	bytesRecv   int64
	rpcCount    int64
}

func MakeNetwork() *Network {
	rn := &Network{
		reliable:    true,
		ended:       map[interface{}]bool{},
		servers:     map[interface{}]*server{},
		connections: map[interface{}](interface{}){},
		endCh:       make(chan reqMsg),
		done:        make(chan struct{}),
	}
	go func() {
		for {
			select {
			case xreq := <-rn.endCh:
				go rn.processReq(xreq)
			case <-rn.done:
				return
			}
		}
	}()
	return rn
}

func (rn *Network) Cleanup() {
	close(rn.done)
}

func (rn *Network) SetReliable(yes bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.reliable = yes
}

func (rn *Network) SetLongDelays(yes bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.longDelays = yes
}

func (rn *Network) Stats() (rpcs int64, sent int64, recv int64) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.rpcCount, rn.bytesSent, rn.bytesRecv
}

// MakeEnd creates a new ClientEnd for a peer named endname (typically the
// numeric server id as a string). It isn't yet Connect()-ed to any server.
func (rn *Network) MakeEnd(endname interface{}) *NetworkEnd {
	e := &NetworkEnd{
		endname: endname,
		ch:      rn.endCh,
		done:    rn.done,
	}
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.ended[endname] = false
	return e
}

// AddServer makes a server (by name) available to be connected to.
func (rn *Network) AddServer(servername interface{}, rs *server) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.servers[servername] = rs
}

func (rn *Network) DeleteServer(servername interface{}) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.servers[servername] = nil
}

// Connect wires an end (client handle) to a server name so RPCs sent on it
// are routed there. Disconnect (or never connecting) simulates that peer
// being unreachable — used to model partitions and crashes.
func (rn *Network) Connect(endname interface{}, servername interface{}) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.connections[endname] = servername
}

func (rn *Network) Enable(endname interface{}, enabled bool) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.ended[endname] = !enabled
}

func (rn *Network) IsServerDead(endname interface{}, servername interface{}, server *server) bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.ended[endname] || rn.servers[servername] != server {
		return true
	}
	return false
}

func (rn *Network) processReq(req reqMsg) {
	rn.mu.Lock()
	enabled := !rn.ended[req.endname]
	servername := rn.connections[req.endname]
	var srv *server
	if servername != nil {
		srv = rn.servers[servername]
	}
	reliable := rn.reliable
	longDelays := rn.longDelays
	rn.mu.Unlock()

	if enabled && srv != nil {
		if !reliable {
			// short delay
			ms := rand.Intn(27)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		if !reliable && (rand.Int()%1000) < 100 {
			// drop the request, return as if timeout
			req.replyCh <- replyMsg{false, nil}
			return
		}
		ech := make(chan replyMsg)
		go func() {
			r := srv.dispatch(req)
			ech <- r
		}()
		var reply replyMsg
		replyOK := false
		serverDead := false
		select {
		case reply = <-ech:
			replyOK = true
		case <-time.After(100 * time.Millisecond):
			serverDead = rn.IsServerDead(req.endname, servername, srv)
			if serverDead {
				go func() { <-ech }()
			}
		}
		serverDead = rn.IsServerDead(req.endname, servername, srv)
		if !replyOK || serverDead {
			req.replyCh <- replyMsg{false, nil}
		} else if !reliable && (rand.Int()%1000) < 100 {
			req.replyCh <- replyMsg{false, nil}
		} else {
			rn.mu.Lock()
			rn.rpcCount++
			rn.mu.Unlock()
			req.replyCh <- reply
		}
	} else {
		ms := 0
		if longDelays {
			ms = rand.Intn(7000)
		} else {
			ms = rand.Intn(100)
		}
		time.AfterFunc(time.Duration(ms)*time.Millisecond, func() {
			req.replyCh <- replyMsg{false, nil}
		})
	}
}

type NetworkEnd struct {
	endname interface{}
	ch      chan reqMsg
	done    chan struct{}
}

func (e *NetworkEnd) Name() string {
	return toName(e.endname)
}

func toName(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "?"
}

// Call marshals args via gob, sends the request through the network, waits
// for a reply, and unmarshals it into reply. Returns false on any failure
// (disconnected end, dead server, dropped packet) — callers must treat
// false exactly like a timeout, per Raft's RPC semantics (retry or move on,
// never assume the peer didn't receive it).
func (e *NetworkEnd) Call(svcMeth string, args interface{}, reply interface{}) bool {
	req := reqMsg{}
	req.endname = e.endname
	req.svcMeth = svcMeth
	req.argsType = reflect.TypeOf(args)
	req.replyCh = make(chan replyMsg)

	qb := new(bytes.Buffer)
	qe := gob.NewEncoder(qb)
	if err := qe.Encode(args); err != nil {
		log.Fatalf("transport: gob encode args: %v", err)
	}
	req.args = qb.Bytes()

	select {
	case e.ch <- req:
	case <-e.done:
		return false
	}

	rep := <-req.replyCh
	if rep.ok {
		rb := bytes.NewBuffer(rep.reply)
		rd := gob.NewDecoder(rb)
		if err := rd.Decode(reply); err != nil {
			log.Fatalf("transport: gob decode reply: %v", err)
		}
		return true
	}
	return false
}

// server holds one node's registered RPC handlers.
type server struct {
	mu       sync.Mutex
	services map[string]*service
	count    int
}

func MakeServer() *server {
	return &server{services: map[string]*service{}}
}

func (rs *server) RegisterName(svcName string, rcvr interface{}) {
	svc := makeService(rcvr)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.services[svcName] = svc
}

func (rs *server) dispatch(req reqMsg) replyMsg {
	rs.mu.Lock()
	rs.count++
	// svcMeth looks like "Raft.RequestVote"
	dot := -1
	for i := len(req.svcMeth) - 1; i >= 0; i-- {
		if req.svcMeth[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		rs.mu.Unlock()
		return replyMsg{false, nil}
	}
	serviceName := req.svcMeth[:dot]
	methodName := req.svcMeth[dot+1:]
	svc, ok := rs.services[serviceName]
	rs.mu.Unlock()
	if !ok {
		return replyMsg{false, nil}
	}
	return svc.dispatch(methodName, req)
}

type service struct {
	typ     reflect.Type
	rcvr    reflect.Value
	methods map[string]reflect.Method
}

func makeService(rcvr interface{}) *service {
	svc := &service{}
	svc.typ = reflect.TypeOf(rcvr)
	svc.rcvr = reflect.ValueOf(rcvr)
	svc.methods = map[string]reflect.Method{}
	for m := 0; m < svc.typ.NumMethod(); m++ {
		method := svc.typ.Method(m)
		svc.methods[method.Name] = method
	}
	return svc
}

func (svc *service) dispatch(methname string, req reqMsg) replyMsg {
	method, ok := svc.methods[methname]
	if !ok {
		return replyMsg{false, nil}
	}
	argsType := method.Type.In(1)
	var argsPtr bool
	if argsType.Kind() == reflect.Ptr {
		argsPtr = true
		argsType = argsType.Elem()
	}
	argsVal := reflect.New(argsType)
	ab := bytes.NewBuffer(req.args)
	ad := gob.NewDecoder(ab)
	if err := ad.Decode(argsVal.Interface()); err != nil {
		log.Fatalf("transport: gob decode args: %v", err)
	}

	replyType := method.Type.In(2).Elem()
	replyVal := reflect.New(replyType)

	var callArgs reflect.Value
	if argsPtr {
		callArgs = argsVal
	} else {
		callArgs = argsVal.Elem()
	}
	function := method.Func
	function.Call([]reflect.Value{svc.rcvr, callArgs, replyVal})

	rb := new(bytes.Buffer)
	re := gob.NewEncoder(rb)
	if err := re.Encode(replyVal.Interface()); err != nil {
		log.Fatalf("transport: gob encode reply: %v", err)
	}
	return replyMsg{true, rb.Bytes()}
}

var errNoSuchServer = errors.New("transport: no such server")
