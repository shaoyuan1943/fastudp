package netpoll

import (
	"sync"

	"golang.org/x/sys/unix"
)

var eventsPool sync.Pool

func epollEventFromPool(n int) []unix.EpollEvent {
	if eventsPool.New == nil {
		eventsPool.New = func() interface{} {
			return make([]unix.EpollEvent, n)
		}
	}

	return eventsPool.Get().([]unix.EpollEvent)
}

func epollEventBackPool(evs []unix.EpollEvent) {
	for i := 0; i < len(evs); i++ {
		evs[i].Events = 0
		evs[i].Fd = 0
		evs[i].Pad = 0
	}

	eventsPool.Put(evs)
}
