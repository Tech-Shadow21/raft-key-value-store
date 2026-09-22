package transport

import (
	"net"
	"net/rpc"
)

// TCPServer wraps net/rpc over TCP for real deployments. Handler types
// registered here must follow net/rpc's export rules (exported method,
// signature func(argType, *replyType) error) — one adapter step away from
// the Network's convention (func(argType, *replyType), no error return)
// because net/rpc requires the error return. raft.Raft and kvstore.Server
// expose thin RPC-shim methods for this reason; see raft/server_rpc.go.
type TCPServer struct {
	rs *rpc.Server
	ln net.Listener
}

func NewTCPServer(addr string) (*TCPServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &TCPServer{rs: rpc.NewServer(), ln: ln}
	go s.rs.Accept(ln)
	return s, nil
}

func (s *TCPServer) RegisterName(svcName string, rcvr interface{}) {
	_ = s.rs.RegisterName(svcName, rcvr)
}

func (s *TCPServer) Addr() string { return s.ln.Addr().String() }

func (s *TCPServer) Close() error { return s.ln.Close() }

// TCPEnd is a ClientEnd backed by a lazily-dialed net/rpc connection.
type TCPEnd struct {
	addr   string
	client *rpc.Client
}

func NewTCPEnd(addr string) *TCPEnd {
	return &TCPEnd{addr: addr}
}

func (e *TCPEnd) Name() string { return e.addr }

func (e *TCPEnd) Call(svcMeth string, args interface{}, reply interface{}) bool {
	if e.client == nil {
		c, err := rpc.Dial("tcp", e.addr)
		if err != nil {
			return false
		}
		e.client = c
	}
	err := e.client.Call(svcMeth, args, reply)
	if err != nil {
		e.client.Close()
		e.client = nil
		return false
	}
	return true
}
