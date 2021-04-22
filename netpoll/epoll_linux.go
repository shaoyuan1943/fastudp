// +build linux

package netpoll

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	EPollEventSize = 128
	EPollErrEvent  = unix.EPOLLERR | unix.EPOLLRDHUP | unix.EPOLLHUP
	EPollInEvent   = EPollErrEvent | unix.EPOLLIN
	EPollOutEvent  = EPollErrEvent | unix.EPOLLOUT
)

type Poller struct {
	fd int
}

func PollerInit() (*Poller, error) {
	fd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, os.NewSyscallError("epoll_create1", err)
	}

	poller := &Poller{
		fd: fd,
	}

	return poller, nil
}

func (poller *Poller) Add(fd int, ev string) error {
	e := &unix.EpollEvent{
		Fd: int32(fd),
	}

	switch ev {
	case "r":
		e.Events = unix.EPOLLIN
	case "w":
		e.Events = unix.EPOLLOUT
	case "rw":
		e.Events = unix.EPOLLIN | unix.EPOLLOUT
	default:
		return fmt.Errorf("unknow epoll event type")
	}

	return os.NewSyscallError("epoll_ctl add", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_ADD, fd, e))
}

func (poller *Poller) Mod(fd int, ev string) error {
	e := &unix.EpollEvent{
		Fd: int32(fd),
	}

	switch ev {
	case "r":
		e.Events = unix.EPOLLIN
	case "w":
		e.Events = unix.EPOLLOUT
	case "rw":
		e.Events = unix.EPOLLIN | unix.EPOLLOUT
	default:
		return fmt.Errorf("unknow epoll event type")
	}

	return os.NewSyscallError("epoll_ctl mod", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_MOD, fd, e))
}

func (poller *Poller) Del(fd int) error {
	return os.NewSyscallError("epoll_ctl del", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_DEL, fd, nil))
}

func (poller *Poller) Close() error {
	return os.NewSyscallError("close", unix.Close(poller.fd))
}

func (poller *Poller) Polling(eventHandler func(fd int32, ev uint32)) error {
	evs := make([]unix.EpollEvent, EPollEventSize)
	for {
		n, err := unix.EpollWait(poller.fd, evs, 0)
		if n < 0 && err == unix.EINTR {
			continue
		}

		if err != nil {
			return os.NewSyscallError("epoll_wait", err)
		}

		for i := 0; i < n; i++ {
			eventHandler(evs[i].Fd, evs[i].Events)
		}
	}
}
