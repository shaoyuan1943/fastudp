// +build linux

package fastudp

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/shaoyuan1943/fastudp/netpoll"
	"github.com/shaoyuan1943/fastudp/netudp"
)

var (
	MsgHdrSize     = 128
	ReadEventSize  = 128
	WriteEventSize = 128
)

type writeP struct {
	Addr *net.UDPAddr
	Data []byte
}

type eventLoop struct {
	internalLoop
	_ [64 - unsafe.Sizeof(internalLoop{})%64]byte
}

type internalLoop struct {
	l            *listener
	poller       *netpoll.Poller
	rw           *netudp.ReaderWriter
	svr          *Server
	once         sync.Once
	readNotifyC  chan struct{}
	writeNotifyC chan *writeP
	writePool    sync.Pool
	closed       atomic.Value
}

func newEventLoop(s *Server, l *listener, poller *netpoll.Poller, mtu int) *eventLoop {
	loop := &eventLoop{}
	loop.l = l
	loop.poller = poller
	loop.rw = netudp.NewRW(l.fd, MsgHdrSize, mtu)
	loop.svr = s
	loop.writeNotifyC = make(chan *writeP, WriteEventSize)
	loop.readNotifyC = make(chan struct{}, ReadEventSize)
	loop.writePool.New = func() interface{} {
		p := &writeP{
			Data: make([]byte, mtu),
		}

		return p
	}
	loop.closed.Store(false)
	return loop
}

func (loop *eventLoop) Close(err error) {
	loop.once.Do(func() {
		loop.poller.Close()
		close(loop.readNotifyC)
		loop.closed.Store(true)
		loop.svr.eventLoopClosed(loop, err)
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

func (loop *eventLoop) writeLoop() {
	for p := range loop.writeNotifyC {
		err := loop.rw.WriteTo(p.Addr, p.Data)
		if err != nil {
			loop.Close(err)
			return
		}

		loop.writePool.Put(p)
	}
}

func (loop *eventLoop) writeTo(addr *net.UDPAddr, data []byte) {
	p := loop.writePool.Get().(*writeP)
	p.Addr = addr
	p.Data = p.Data[:len(data)]
	copy(p.Data, data)
	loop.writeNotifyC <- p
}
