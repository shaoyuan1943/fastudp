// +build linux

package fastudp

import (
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/shaoyuan1943/fastudp/netpoll"
	"github.com/shaoyuan1943/fastudp/netudp"
	"golang.org/x/sys/unix"
)

var (
	MsgHdrSize     = 128
	ReadEventSize  = 128
	WriteEventSize = 128
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
	writePool   sync.Pool
	closed      atomic.Value
	writeQueue  []*netudp.Mmsg
	sync.Locker
}

func newEventLoop(s *Server, l *listener, poller *netpoll.Poller, mtu int) *eventLoop {
	loop := &eventLoop{}
	loop.l = l
	loop.poller = poller
	loop.rw = netudp.NewRW(l.fd, MsgHdrSize, mtu)
	loop.svr = s
	loop.readNotifyC = make(chan struct{}, ReadEventSize)
	loop.writePool.New = func() interface{} {
		p := &netudp.Mmsg{
			Data: make([]byte, mtu),
		}

		return p
	}
	loop.writeQueue = make([]*netudp.Mmsg, WriteEventSize)
	loop.writeQueue = loop.writeQueue[:0]
	loop.closed.Store(false)
	return loop
}

func (loop *eventLoop) Close(err error) {
	loop.once.Do(func() {
		loop.poller.Del(loop.l.fd)
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

// Epoll return current status of fd,
// EPOLLOUT will only be returned when the fd's status changes from "cannnot ouput" to "can ouput",
// More information: https://www.spinics.net/lists/linux-api/msg01872.html
func (loop *eventLoop) pollEvent(fd int32, events uint32) {
	if fd == int32(loop.l.fd) && netudp.IsUDP(loop.l.network) {
		if !loop.closed.Load().(bool) {
			if events&unix.EPOLLIN != 0 {
				loop.readNotifyC <- struct{}{}
			}

			if events&unix.EPOLLOUT != 0 {
				loop.onEpollout()
			}
		}
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

func (loop *eventLoop) writeTo(data []byte, addr *net.UDPAddr) {
	err := loop.rw.WriteTo(data, addr)
	if err != nil {
		errno := err.(*os.SyscallError).Unwrap()
		if errno.Error() == "EINTR" || errno.Error() == "EAGIN" {
			loop.Lock()
			defer loop.Unlock()

			p := loop.writePool.Get().(*netudp.Mmsg)
			p.Addr = addr
			p.Data = p.Data[:len(data)]
			copy(p.Data, data)

			loop.writeQueue = append(loop.writeQueue, p)
			loop.poller.Mod(int(loop.l.fd), "rw")
		} else {
			loop.Close(err)
		}
	}
}

func (loop *eventLoop) onEpollout() {
	n := len(loop.writeQueue)
	if n <= 0 {
		return
	}

	writeFunc := func(mmsgs []*netudp.Mmsg) (writed bool) {
		if len(mmsgs) > 0 {
			_, err := loop.rw.WriteToN(mmsgs...)
			if err != nil {
				errno := err.(*os.SyscallError).Unwrap()
				if errno.Error() == "EINTR" || errno.Error() == "EAGIN" {
					loop.poller.Mod(int(loop.l.fd), "rw")
					writed = false
					return
				}

				loop.Close(err)
				writed = false
				return
			}
		}

		writed = true
		return
	}

	putback := func(mmsgs []*netudp.Mmsg) {
		for i := 0; i < len(mmsgs); i++ {
			loop.writePool.Put(mmsgs[i])
		}
	}

	loop.Lock()
	defer loop.Unlock()

	if n <= WriteEventSize {
		if !writeFunc(loop.writeQueue) {
			return
		}

		putback(loop.writeQueue)
		loop.writeQueue = loop.writeQueue[:0]
		loop.poller.Mod(int(loop.l.fd), "r")
	} else {
		step := n / WriteEventSize
		surplus := n % WriteEventSize
		if surplus > 0 {
			if !writeFunc(loop.writeQueue[(n - surplus):n]) {
				return
			}

			putback(loop.writeQueue[(n - surplus):n])
			loop.writeQueue = loop.writeQueue[:(n - surplus)]
			loop.poller.Mod(int(loop.l.fd), "r")
		}

		n := len(loop.writeQueue)
		step--
		for step >= 0 {
			if !writeFunc(loop.writeQueue[step*WriteEventSize : n]) {
				return
			}

			putback(loop.writeQueue[step*WriteEventSize : n])
			loop.writeQueue = loop.writeQueue[:step*WriteEventSize]
			loop.poller.Mod(int(loop.l.fd), "r")
			n = len(loop.writeQueue)
			step--
		}
	}
}
