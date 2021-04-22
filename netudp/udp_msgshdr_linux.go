// +build linux

package netudp

import "unsafe"

// This file content copied from golang.org/x/net/internal/socket/zsys_linux_amd64.go
// Don't modify only when you know what to do

type iovec struct {
	Base *byte
	Len  uint64
}

type msghdr struct {
	Name       *byte
	Namelen    uint32
	Pad_cgo_0  [4]byte
	Iov        *iovec
	Iovlen     uint64
	Control    *byte
	Controllen uint64
	Flags      int32
	Pad_cgo_1  [4]byte
}

type mmsghdr struct {
	Hdr       msghdr
	Len       uint32
	Pad_cgo_0 [4]byte
}

type sockaddrFamily struct {
	Family uint16
}

const (
	sizeofIovec  = 0x10
	sizeofMsghdr = 0x38

	sizeofSockaddrInet  = 0x10 // IPv4
	sizeofSockaddrInet6 = 0x1c // IPv6
)

func prepare(n, mtu int) ([]mmsghdr, [][]byte, [][]byte) {
	mms := make([]mmsghdr, n)
	buffers := make([][]byte, n)
	names := make([][]byte, n)

	for i := range mms {
		buffers[i] = make([]byte, mtu)
		names[i] = make([]byte, sizeofSockaddrInet6)

		v := []iovec{
			{Base: (*byte)(unsafe.Pointer(&buffers[i][0])), Len: uint64(len(buffers[i]))},
		}

		mms[i].Hdr.Iov = &v[0]
		mms[i].Hdr.Iovlen = uint64(len(v))

		mms[i].Hdr.Name = (*byte)(unsafe.Pointer(&names[i][0]))
		mms[i].Hdr.Namelen = uint32(len(names[i]))

		// ignore mms[i].Hdr.Control and mms[i].Hdr.Controllen
	}

	return mms, buffers, names
}
