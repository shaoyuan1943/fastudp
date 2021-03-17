package fastudp

import (
	"net"
	"runtime"
	"sync"
	"unsafe"

	"github.com/shaoyuan1943/fastudp/netpoll"
	"github.com/shaoyuan1943/fastudp/netudp"
)

var (
	MsgHdrSize    = 128
	ReadEventSize = 128
)

type eventLoop struct {
	internalLoop
	_ [64 - unsafe.Sizeof(internalLoop{})%64]byte
}

type internalLoop struct {
	l           *listener
	poller      *netpoll.Poller
	rw          *netudp.ReaderWriter
	svr         *Server
	once        sync.Once
	readNotifyC chan struct{}
	reasonErr   error
	closed      bool
}

func newEventLoop(s *Server, l *listener, poller *netpoll.Poller, mtu int) *eventLoop {
	loop := &eventLoop{}
	loop.l = l
	loop.poller = poller
	loop.rw = netudp.NewRW(l.fd, MsgHdrSize, mtu)
	loop.svr = s
	loop.readNotifyC = make(chan struct{}, ReadEventSize)
	return loop
}

func (loop *eventLoop) Close(err error) {
	loop.once.Do(func() {
		loop.reasonErr = err
		loop.poller.Close()
		close(loop.readNotifyC)
		loop.closed = true
	})
}

func (loop *eventLoop) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err := loop.poller.Polling(loop.pollEvent)
	loop.Close(err)
}

func (loop *eventLoop) pollEvent(fd int32, event uint32) {
	if fd == int32(loop.l.fd) && netudp.IsUDP(loop.l.network) && !loop.closed {
		loop.readNotifyC <- struct{}{}
	}
}

func (loop *eventLoop) readLoop() {
	for _ = range loop.readNotifyC {
		loop.rw.ReadFrom(func(data []byte, addr *net.UDPAddr, err error) {
			if err != nil {
				loop.Close(err)
				return
			}

			loop.svr.handler.OnReaded(data, addr)
		})
	}
}
