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

	svr := &Server{
		handler: handler,
	}

	if err := svr.start(network, addr, mtu); err != nil {
		return nil, err
	}

	svr.closed.Store(false)
	return svr, nil
}

func (svr *Server) start(network, addr string, mtu int) error {
	udpAddr, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %v", err)
	}

	conn, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %v", err)
	}

	svr.conn = conn

	svr.wg.Add(1)
	go func() {
		defer svr.wg.Done()

		buffer := make([]byte, mtu)
		for {
			n, remoteAddr, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}

			if n > 0 {
				svr.handler.OnReaded(buffer[:n], remoteAddr.(*net.UDPAddr))
			}

		}
	}()

	return nil
}

func (svr *Server) Shutdown(err error) {
	if svr.IsClosed() {
		return
	}

	svr.conn.Close()
	svr.wg.Wait()
	svr.closed.Store(true)
}

func (svr *Server) IsClosed() bool {
	return svr.closed.Load().(bool)
}

func (svr *Server) WriteTo(addr *net.UDPAddr, data []byte) (int, error) {
	if svr.closed.Load().(bool) {
		return 0, fmt.Errorf("server is closed")
	}

	return svr.conn.WriteTo(data, addr)
}
