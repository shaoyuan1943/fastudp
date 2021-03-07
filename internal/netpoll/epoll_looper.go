package netpoll

import "unsafe"

type epollLooper struct {
	innerLooper
	padding [cpu.CacheLinePadSize - unsafe.Sizeof(innerLooper{})%cpu.CacheLinePadSize]byte
}

type innerLooper struct {
}
