// +build linux
// +build amd64

package fastudp

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type ReadFunc func(net.Addr, []byte, error)

type UDPConn struct {
	fd int
}

func NewUDPConn(network, addr string) (*UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		return nil, fmt.Errorf("resolov addr err: %v", err)
	}

	afNet := unix.AF_INET
	isIPv6 := false
	if udpAddr.IP.To4() == nil {
		isIPv6 = true
		afNet = unix.AF_INET6
	}

	syscall.ForkLock.Lock()
	fd, err := unix.Socket(afNet, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	if err == nil {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.Unlock()

	if isIPv6 {
		sockaddr := &unix.SockaddrInet6{}
		copy(sockaddr.Addr[:], udpAddr.IP.To16())
		if udpAddr.Zone != "" {
			var iface *net.Interface
			iface, err = net.InterfaceByName(udpAddr.Zone)
			if err != nil {
				return nil, fmt.Errorf("parse UDPAddr.Zone err: %v", err)
			}
			sockaddr.ZoneId = uint32(iface.Index)
		}
		sockaddr.Port = udpAddr.Port
		err = unix.Bind(fd, sockaddr)
		if err != nil {
			return nil, fmt.Errorf("bind addr err: %v", err)
		}
	} else {
		sockaddr := &unix.SockaddrInet4{}
		copy(sockaddr.Addr[:], udpAddr.IP.To4())
		sockaddr.Port = udpAddr.Port
		err = unix.Bind(fd, sockaddr)
		if err != nil {
			return nil, fmt.Errorf("bind addr err: %v", err)
		}
	}

	return &UDPConn{fd: fd}, nil
}

func (conn *UDPConn) SetRecvBufferLen(n int) error {
	return unix.SetsockoptInt(conn.fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, n)
}

func (conn *UDPConn) SetSendBufferLen(n int) error {
	return unix.SetsockoptInt(conn.fd, unix.SOL_SOCKET, unix.SO_SNDBUFFORCE, n)
}

// n MUST more than 1
func (conn *UDPConn) ReadFrom(n int, mtu int, readFunc ReadFunc) {
	msgs, buffers, names := readPrepare(n, mtu)
	for {
		n, err := conn.readMultiMsg(msgs)
		if err != nil {
			readFunc(nil, nil, err)
			continue
		}

		for i := 0; i < n; i++ {
			udpAddr := &net.UDPAddr{}
			familyData := names[i][:2]
			afNet := (*sockaddrFamily)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&familyData)).Data))
			if afNet.Family == unix.AF_INET {
				sockaddrInet := (*unix.RawSockaddrInet4)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&names[i])).Data))
				udpAddr.IP = sockaddrInet.Addr[:]
				udpAddr.Port = int(sockaddrInet.Port)
			} else if afNet.Family == unix.AF_INET6 {
				sockaddrInet6 := (*unix.RawSockaddrInet6)(unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&names[i])).Data))
				udpAddr.IP = sockaddrInet6.Addr[:]
				udpAddr.Port = int(sockaddrInet6.Port)
				udpAddr.Zone = strconv.Itoa(int(sockaddrInet6.Scope_id))
			} else {
				panic(fmt.Errorf("not support net family other than AF_NET and AF_NET6"))
			}

			readFunc(udpAddr, buffers[i][:msgs[i].Len], err)
		}
	}
}

func (conn *UDPConn) readMultiMsg(msgs []mmsghdr) (int, error) {
	for {
		n, _, err := unix.Syscall6(
			unix.SYS_RECVMMSG, uintptr(conn.fd),
			uintptr(unsafe.Pointer(&msgs[0])),
			uintptr(len(msgs)), unix.MSG_WAITFORONE,
			0, 0,
		)

		if err != 0 {
			return 0, &net.OpError{Op: "Read", Err: err}
		}

		return int(n), nil
	}
}

func (conn *UDPConn) WriteTo(data []byte, addr *net.UDPAddr) error {
	isIPv6 := false
	if addr.IP.To4() == nil {
		isIPv6 = true
	}

	var targetSockaddrP uintptr
	var targetSockaddrSizeP uintptr
	if isIPv6 {
		rsa := unix.RawSockaddrInet6{}
		rsa.Family = unix.AF_INET6
		zoneID, err := strconv.Atoi(addr.Zone)
		if err != nil {
			return fmt.Errorf("invalid net.UDPAddr in WriteTo")
		}
		rsa.Scope_id = uint32(zoneID)

		port := (*[2]byte)(unsafe.Pointer(&rsa.Port))
		port[0] = byte(addr.Port >> 8)
		port[1] = byte(addr.Port)

		copy(rsa.Addr[:], addr.IP)

		targetSockaddrP = uintptr(unsafe.Pointer(&rsa))
		targetSockaddrSizeP = uintptr(unix.SizeofSockaddrInet6)
	} else {
		rsa := unix.RawSockaddrInet4{}
		rsa.Family = unix.AF_INET

		port := (*[2]byte)(unsafe.Pointer(&rsa.Port))
		port[0] = byte(addr.Port >> 8)
		port[1] = byte(addr.Port)

		copy(rsa.Addr[:], addr.IP)

		targetSockaddrP = uintptr(unsafe.Pointer(&rsa))
		targetSockaddrSizeP = uintptr(unix.SizeofSockaddrInet4)
	}

	for {
		_, _, err := unix.Syscall6(
			unix.SYS_SENDTO, uintptr(conn.fd), uintptr(unsafe.Pointer(&data[0])),
			uintptr(len(data)), uintptr(0),
			targetSockaddrP, targetSockaddrSizeP,
		)

		if err != 0 {
			return &net.OpError{Op: "sendto", Err: err}
		}

		return nil
	}
}

func (conn *UDPConn) Close() {
	if conn.fd != 0 {
		unix.CloseOnExec(conn.fd)
	}
}
