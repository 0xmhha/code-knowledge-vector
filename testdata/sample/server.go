package sample

import (
	"errors"
	"net"
)

// Server holds a TCP listener for the demo service.
type Server struct {
	addr     string
	listener net.Listener
}

// NewServer constructs a Server bound to the given address.
func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

// Listen opens a TCP socket bound to s.addr. Used by the integration
// test to verify CKV finds this function for the query "TCP socket
// bind on port".
func (s *Server) Listen() error {
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = l
	return nil
}

// Close shuts down the listener if it has been opened.
func (s *Server) Close() error {
	if s.listener == nil {
		return errors.New("server: not listening")
	}
	return s.listener.Close()
}
