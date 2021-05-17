// +build linux

package netpoll

import (
	"fmt"
	"os"
	"runtime"

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
		e.Events = unix.EPOLLIN | unix.EPOLLET
	case "w":
		e.Events = unix.EPOLLOUT | unix.EPOLLET
	case "rw":
		e.Events = unix.EPOLLIN | unix.EPOLLOUT | unix.EPOLLET
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
		e.Events = unix.EPOLLIN | unix.EPOLLET
	case "w":
		e.Events = unix.EPOLLOUT | unix.EPOLLET
	case "rw":
		e.Events = unix.EPOLLIN | unix.EPOLLOUT | unix.EPOLLET
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
		// See: https://man7.org/linux/man-pages/man2/epoll_pwait2.2.html
		// In Go, if epoll_wait not return event or interrupted by a signal,
		// We can hand over to the runtime for scheduling to educe CPU usage
		msec := -1
		n, err := unix.EpollWait(poller.fd, evs, msec)
		if n == 0 || (n < 0 && err == unix.EINTR) {
			msec = -1
			runtime.Gosched()
			continue
		}

		if err != nil {
			return os.NewSyscallError("epoll_wait", err)
		}

		msec = 0
		for i := 0; i < n; i++ {
			eventHandler(evs[i].Fd, evs[i].Events)
		}
	}
}
