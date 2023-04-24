//go:build linux
// +build linux

package netudp

import (
	"fmt"
	"net"
	"os"
	"reflect"
	"unsafe"

	"golang.org/x/sys/unix"
)

var zero [32]byte

type Mmsg struct {
	Addr *net.UDPAddr
	Data []byte
}

type ReaderWriter struct {
	fd         int
	msgs       []mmsghdr
	buffers    [][]byte
	names      [][]byte
	remoteAddr *net.UDPAddr
	dc         [32]byte
	mtu        int
	sockaddr4  unix.RawSockaddrInet4
	sockaddr6  unix.RawSockaddrInet6
}

func NewRW(fd, n, mtu int) *ReaderWriter {
	rw := &ReaderWriter{}
	rw.fd = fd
	rw.mtu = mtu
	rw.msgs, rw.buffers, rw.names = prepare(n, mtu)
	rw.remoteAddr = &net.UDPAddr{}
	return rw
}

func (rw *ReaderWriter) ReadFrom(readFunc func([]byte, *net.UDPAddr, error)) {
	n, err := rw.read()
	if err != nil {
		readFunc(nil, nil, err)
		return
	}

	for i := 0; i < n; i++ {
		familyData := rw.names[i][:2]
		afNet := (*sockaddrFamily)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&familyData)).Data))
		switch afNet.Family {
		case unix.AF_INET:
			rw.names[i][2], rw.names[i][3] = rw.names[i][3], rw.names[i][2] //字节序转换
			sockaddrInet := (*unix.RawSockaddrInet4)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&rw.names[i])).Data))
			rw.remoteAddr.IP = sockaddrInet.Addr[:]
			rw.remoteAddr.Port = int(sockaddrInet.Port)
		case unix.AF_INET6:
			rw.names[i][2], rw.names[i][3] = rw.names[i][3], rw.names[i][2] //字节序转换
			sockaddrInet6 := (*unix.RawSockaddrInet6)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&rw.names[i])).Data))
			rw.remoteAddr.IP = sockaddrInet6.Addr[:]
			rw.remoteAddr.Port = int(sockaddrInet6.Port)
			rw.remoteAddr.Zone = rw.zoneID2String(int(sockaddrInet6.Scope_id))
		default:
			err := fmt.Errorf("unknown net family")
			readFunc(nil, nil, err)
			return
		}

		readFunc(rw.buffers[i][:rw.msgs[i].Len], rw.remoteAddr, nil)

		if afNet.Family == unix.AF_INET6 {
			copy(rw.dc[:], zero[:])
		}
	}
}

// See: https://www.man7.org/linux/man-pages/man2/recvmmsg.2.html
func (rw *ReaderWriter) read() (int, error) {
	n, _, err := unix.Syscall6(unix.SYS_RECVMMSG, uintptr(rw.fd),
		uintptr(unsafe.Pointer(&rw.msgs[0])), uintptr(len(rw.msgs)), unix.MSG_WAITFORONE,
		0, 0,
	)

	if err != 0 {
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			return 0, nil
		}

		return 0, os.NewSyscallError("recvmmsg", fmt.Errorf("%v", unix.ErrnoName(err)))
	}

	return int(n), nil
}

func (rw *ReaderWriter) zoneID2String(zoneID int) string {
	if zoneID == 0 {
		return ""
	}

	if ifi, err := net.InterfaceByIndex(int(zoneID)); err == nil {
		return ifi.Name
	}

	n := len(rw.dc)
	for ; zoneID > 0; zoneID /= 10 {
		n--
		rw.dc[n] = byte(zoneID%10) + '0'
	}

	return string(rw.dc[n:])
}

func dtoi(s string, i0 int) (n int, i int, ok bool) {
	n = 0
	for i = i0; i < len(s) && '0' <= s[i] && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
		if n >= 0xFFFFFF {
			return 0, i, false
		}
	}

	if i == i0 {
		return 0, i, false
	}

	return n, i, true
}

func (rw *ReaderWriter) string2ZoneID(zone string) uint32 {
	if zone == "" {
		return 0
	}

	if ifi, err := net.InterfaceByName(zone); err == nil {
		return uint32(ifi.Index)
	}

	n, _, _ := dtoi(zone, 0)
	return uint32(n)
}

func (rw *ReaderWriter) WriteTo(data []byte, addr *net.UDPAddr) error {
	if addr == nil || data == nil {
		return fmt.Errorf("writeto: data or addr invalid")
	}

	if len(data) > rw.mtu {
		return fmt.Errorf("writeto: data length too long")
	}

	if addr.IP.To4() == nil {
		return rw.writeToIPv6(data, addr)
	}

	return rw.writeToIPv4(data, addr)
}

func (rw *ReaderWriter) writeToIPv4(data []byte, addr *net.UDPAddr) error {
	rw.sockaddr4.Family = unix.AF_INET
	port := (*[2]byte)(unsafe.Pointer(&rw.sockaddr4.Port))
	port[0] = byte(addr.Port >> 8)
	port[1] = byte(addr.Port)

	copy(rw.sockaddr4.Addr[:], addr.IP)

	return rw.writeto(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(0), uintptr(unsafe.Pointer(&rw.sockaddr4)), uintptr(unix.SizeofSockaddrInet4))
}

func (rw *ReaderWriter) writeToIPv6(data []byte, addr *net.UDPAddr) error {
	rw.sockaddr6.Family = unix.AF_INET6
	rw.sockaddr6.Scope_id = rw.string2ZoneID(addr.Zone)
	port := (*[2]byte)(unsafe.Pointer(&rw.sockaddr6.Port))
	port[0] = byte(addr.Port >> 8)
	port[1] = byte(addr.Port)

	copy(rw.sockaddr6.Addr[:], addr.IP)

	return rw.writeto(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(0), uintptr(unsafe.Pointer(&rw.sockaddr6)), uintptr(unix.SizeofSockaddrInet6))
}

func (rw *ReaderWriter) writeto(data uintptr, dataLen uintptr, flags uintptr, sockaddr uintptr, sockaddrSize uintptr) error {
	_, _, err := unix.Syscall6(unix.SYS_SENDTO, uintptr(rw.fd), data, dataLen, flags, sockaddr, sockaddrSize)
	if err != 0 {
		return os.NewSyscallError("sendto", fmt.Errorf("%v", unix.ErrnoName(err)))
	}

	return nil
}

// See: https://man7.org/linux/man-pages/man2/sendmmsg.2.html
func (rw *ReaderWriter) WriteToN(mmsgs ...*Mmsg) (int, error) {
	writed := 0
	n := len(mmsgs)
	mms := make([]mmsghdr, n)
	for i := 0; i < n; i++ {
		msg := mmsgs[i]
		if msg.Addr.IP.To4() == nil { // IPv6
			sockaddrInet6 := &unix.RawSockaddrInet6{}
			sockaddrInet6.Family = unix.AF_INET6
			sockaddrInet6.Scope_id = rw.string2ZoneID(msg.Addr.Zone)
			port := (*[2]byte)(unsafe.Pointer(&sockaddrInet6.Port))
			port[0] = byte(msg.Addr.Port >> 8)
			port[1] = byte(msg.Addr.Port)
			copy(sockaddrInet6.Addr[:], msg.Addr.IP)

			l := unsafe.Sizeof(*sockaddrInet6)
			m := &reflect.SliceHeader{
				Data: uintptr(unsafe.Pointer(sockaddrInet6)),
				Cap:  int(l),
				Len:  int(l),
			}
			name := *(*[]byte)(unsafe.Pointer(m))
			mms[i].Hdr.Name = (*byte)(unsafe.Pointer(&name[0]))
			mms[i].Hdr.Namelen = uint32(len(name))

			if len(msg.Data) > rw.mtu {
				msg.Data = msg.Data[:rw.mtu]
			}
		} else { // IPv4
			sockaddrInet4 := &unix.RawSockaddrInet4{}
			sockaddrInet4.Family = unix.AF_INET
			port := (*[2]byte)(unsafe.Pointer(&sockaddrInet4.Port))
			port[0] = byte(msg.Addr.Port >> 8)
			port[1] = byte(msg.Addr.Port)
			copy(rw.sockaddr4.Addr[:], msg.Addr.IP)

			l := unsafe.Sizeof(*sockaddrInet4)
			m := &reflect.SliceHeader{
				Data: uintptr(unsafe.Pointer(sockaddrInet4)),
				Cap:  int(l),
				Len:  int(l),
			}
			name := *(*[]byte)(unsafe.Pointer(m))
			mms[i].Hdr.Name = (*byte)(unsafe.Pointer(&name[0]))
			mms[i].Hdr.Namelen = uint32(len(name))
		}

		v := []iovec{
			{Base: (*byte)(unsafe.Pointer(&msg.Data[0])), Len: uint64(len(msg.Data))},
		}

		mms[i].Hdr.Iov = &v[0]
		mms[i].Hdr.Iovlen = uint64(len(v))
		writed++
	}

	_, _, err := unix.Syscall6(unix.SYS_SENDMMSG, uintptr(rw.fd), uintptr(unsafe.Pointer(&mms[0])), uintptr(len(mms)), uintptr(0), 0, 0)
	if err != 0 {
		return 0, os.NewSyscallError("sendmmsg", fmt.Errorf("%v", unix.ErrnoName(err)))
	}

	return writed, nil
}
