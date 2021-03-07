package netpoll

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	EPollErrEvent = unix.EPOLLERR | unix.EPOLLRDHUP | unix.EPOLLHUP
	EPollInEvent  = EPollErrEvent | unix.EPOLLIN
	EPollOutEvent = EPollErrEvent | unix.EPOLLOUT
)

var (
	eventPool sync.Pool
)

type Poller struct {
	fd        int
	eventSize int
}

func OpenPoller(n int) (*Poller, error) {
	fd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, os.NewSyscallError("epoll_create1", err)
	}

	poller := &Poller{
		fd:        fd,
		eventSize: n,
	}

	return poller, nil
}

// ev: "r" read, "w" write, "rw" read-write
func (poller *Poller) Add(fd int, ev string) error {
	event := &unix.EpollEvent{
		Fd: int32(fd),
	}

	switch ev {
	case "r":
		event.Events = unix.EPOLLIN
	case "w":
		event.Events = unix.EPOLLOUT
	case "rw":
		event.Events = unix.EPOLLIN | unix.EPOLLOUT
	default:
		return fmt.Errorf("unknow epoll event type")
	}

	return os.NewSyscallError("epoll_ctl add", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_ADD, fd, event))
}

func (poller *Poller) Mod(fd int, ev string) error {
	event := &unix.EpollEvent{
		Fd: int32(fd),
	}

	switch ev {
	case "r":
		event.Events = unix.EPOLLIN
	case "w":
		event.Events = unix.EPOLLOUT
	case "rw":
		event.Events = unix.EPOLLIN | unix.EPOLLOUT
	default:
		return fmt.Errorf("unknow epoll event type")
	}

	return os.NewSyscallError("epoll_ctl mod", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_MOD, fd, event))
}

func (poller *Poller) Del(fd int) error {
	return os.NewSyscallError("epoll_ctl del", unix.EpollCtl(poller.fd, unix.EPOLL_CTL_DEL, fd, nil))
}

func (poller *Poller) Polling(handler func(fd int32, ev uint32)) error {
	evs := epollEventFromPool(poller.eventSize)
	for {
		n, err := unix.EpollWait(poller.fd, evs, -1)
		if err != nil {
			return os.NewSyscallError("epoll_wait", err)
		}

		for i := 0; i < n; i++ {
			handler(evs[i].Fd, evs[i].Events)
		}
	}
}
