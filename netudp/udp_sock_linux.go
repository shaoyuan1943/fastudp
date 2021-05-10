// +build linux

package netudp

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func NewUDPSocket(network, addr string, reusePort bool) (int, unix.Sockaddr, error) {
	var sa unix.Sockaddr
	udpAddr, err := net.ResolveUDPAddr(network, addr)
	if err != nil {
		return 0, nil, fmt.Errorf("resolve addr err: %v", err)
	}

	netFamily := unix.AF_INET
	if udpAddr.IP.To4() == nil {
		netFamily = unix.AF_INET6
	}

	syscall.ForkLock.Lock()
	fd, err := unix.Socket(netFamily, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.IPPROTO_UDP)
	if err == nil {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.Unlock()

	defer func() {
		if err != nil {
			_ = unix.Close(fd)
		}
	}()

	switch network {
	case "udp":
		sockaddr := &unix.SockaddrInet4{}
		sockaddr.Port = udpAddr.Port
		sa = sockaddr
	case "udp4":
		sockaddr := &unix.SockaddrInet4{}
		sockaddr.Port = udpAddr.Port
		copy(sockaddr.Addr[:], udpAddr.IP.To4())
		sa = sockaddr
	case "udp6":
		sockaddr := &unix.SockaddrInet6{}
		copy(sockaddr.Addr[:], udpAddr.IP.To16())
		if udpAddr.Zone != "" {
			var iface *net.Interface
			iface, err = net.InterfaceByName(udpAddr.Zone)
			if err != nil {
				return 0, nil, fmt.Errorf("parse UDPAddr.Zone err: %v", err)
			}
			sockaddr.ZoneId = uint32(iface.Index)
		}
		sockaddr.Port = udpAddr.Port
		netFamily = unix.AF_INET6
	default:
		return 0, nil, fmt.Errorf("not support network")
	}

	if reusePort {
		if err = os.NewSyscallError("setsockopt", unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)); err != nil {
			return 0, nil, err
		}
	}

	if err = os.NewSyscallError("bind", unix.Bind(fd, sa)); err != nil {
		return 0, nil, err
	}

	return fd, sa, nil
}
