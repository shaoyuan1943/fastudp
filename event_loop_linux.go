//go:build linux
// +build linux

package fastudp

import (
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/shaoyuan1943/fastudp/netpoll"
	"github.com/shaoyuan1943/fastudp/netudp"
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

func (loop *eventLoop) run(lockThread bool) {
	if lockThread {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

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

func (loop *eventLoop) writeTo(data []byte, addr *net.UDPAddr) (int, error) {
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
			loop.poller.Mod(loop.l.fd, "rw")
			err = errno
		} else {
			loop.Close(err)
		}
	}

	return len(data), err
}

// write return value
const (
	retry   = 1 << iota
	failed  = 1 << iota
	succeed = 1 << iota
)

func (loop *eventLoop) onEpollout() {
	n := len(loop.writeQueue)
	if n <= 0 {
		return
	}

	writeFunc := func(mmsgs []*netudp.Mmsg) (writed int) {
		if len(mmsgs) > 0 {
			_, err := loop.rw.WriteToN(mmsgs...)
			if err != nil {
				errno := err.(*os.SyscallError).Unwrap()
				if errno.Error() == "EINTR" || errno.Error() == "EAGIN" {
					writed = retry
					return
				}

				loop.Close(err)
				writed = failed
				return
			}
		}

		writed = succeed
		return
	}

	returnBackFunc := func(mmsgs []*netudp.Mmsg) {
		for i := 0; i < len(mmsgs); i++ {
			loop.writePool.Put(mmsgs[i])
		}
	}

	loop.Lock()
	defer loop.Unlock()

	if n <= WriteEventSize {
		switch writeFunc(loop.writeQueue) {
		case retry:
		case failed:
			return
		case succeed:
			returnBackFunc(loop.writeQueue)
			loop.writeQueue = loop.writeQueue[:0]
		}
	} else {
		step := n / WriteEventSize
		surplus := n % WriteEventSize
		if surplus > 0 {
			switch writeFunc(loop.writeQueue[(n - surplus):n]) {
			case retry:
			case failed:
				return
			case succeed:
				returnBackFunc(loop.writeQueue[(n - surplus):n])
				loop.writeQueue = loop.writeQueue[:(n - surplus)]
			}
		}

		n := len(loop.writeQueue)
		step--
		for step >= 0 {
			switch writeFunc(loop.writeQueue[step*WriteEventSize : n]) {
			case retry:
			case failed:
				return
			case succeed:
				returnBackFunc(loop.writeQueue[step*WriteEventSize : n])
				loop.writeQueue = loop.writeQueue[:step*WriteEventSize]
				n = len(loop.writeQueue)
				step--
			}
		}
	}

	loop.poller.Mod(loop.l.fd, "rw")
}
