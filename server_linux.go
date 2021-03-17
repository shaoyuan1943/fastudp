package fastudp

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/shaoyuan1943/fastudp/netpoll"
	"github.com/shaoyuan1943/fastudp/netudp"
)

type Server struct {
	wg      sync.WaitGroup
	handler EventHandler
	loops   []*eventLoop
}

func NewUDPServer(network, addr string, reusePort bool, listenerN int, mtu int, handler EventHandler) (*Server, error) {
	if !netudp.IsUDP(network) {
		return nil, fmt.Errorf("unknown network: %v", network)
	}

	s := &Server{
		handler: handler,
	}

	if reusePort {
		if listenerN <= 0 {
			listenerN = runtime.NumCPU()
		}
	} else {
		listenerN = 1
	}

	if err := s.start(network, addr, reusePort, listenerN, mtu); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) start(network, addr string, reusePort bool, listenerN, mtu int) error {
	s.loops = make([]*eventLoop, 0)
	for i := 0; i < listenerN; i++ {
		l, err := listen(network, addr, reusePort)
		if err != nil {
			return err
		}

		poller, err := netpoll.PollerInit()
		if err != nil {
			return err
		}

		loop := newEventLoop(s, l, poller, mtu)
		s.loops = append(s.loops, loop)
		poller.Add(loop.l.fd, "r")

		go loop.readLoop()

		s.wg.Add(1)
		go func() {
			loop.run()
			s.wg.Done()
		}()
	}

	return nil
}

func (s *Server) Shutdown() {
	for _, loop := range s.loops {
		if loop != nil {
			loop.Close(nil)
		}
	}

	s.wg.Wait()
}

func (s *Server) onLoopClosed(loop *eventLoop) {
}
