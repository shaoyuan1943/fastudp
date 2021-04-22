package fastudp

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/shaoyuan1943/fastudp/netudp"
)

type Server struct {
	wg      sync.WaitGroup
	handler EventHandler
	conn    *net.UDPConn
	err     error
	closed  atomic.Value
}

func NewUDPServer(network, addr string, mtu int, handler EventHandler) (*Server, error) {
	if !netudp.IsUDP(network) {
		return nil, fmt.Errorf("unknown network: %v", network)
	}

	s := &Server{
		handler: handler,
	}
	s.closed.Store(false)
	if err := s.start(network, addr, mtu); err != nil {
		return nil, fmt.Errorf("%v", err)
	}

	return s, nil
}

func (s *Server) start(network, addr string, mtu int) error {
	udpAddr, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %v", err)
	}

	conn, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %v", err)
	}

	s.conn = conn

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		buffer := make([]byte, mtu)
		for {
			n, remoteAddr, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}

			if n > 0 {
				s.handler.OnReaded(buffer[:n], remoteAddr.(*net.UDPAddr))
			}

		}
	}()

	return nil
}

func (s *Server) Shutdown(err error) {
	if s.closed.Load().(bool) {
		return
	}

	s.conn.Close()
	s.wg.Wait()
	s.closed.Store(true)
}

func (s *Server) IsClosed() bool {
	return s.closed.Load().(bool)
}

func (s *Server) WriteTo(addr *net.UDPAddr, data []byte) (int, error) {
	if s.closed.Load().(bool) {
		return 0, fmt.Errorf("server is closed")
	}

	return s.conn.WriteTo(data, addr)
}
