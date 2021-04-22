// +build linux

package fastudp

import (
	"github.com/shaoyuan1943/fastudp/netudp"
	"golang.org/x/sys/unix"
)

type listener struct {
	addr    unix.Sockaddr
	fd      int
	network string
}

func listen(network, addr string, reusePort bool) (*listener, error) {
	l := &listener{}
	fd, sockaddr, err := netudp.NewUDPSocket(network, addr, reusePort)
	if err != nil {
		return nil, err
	}

	l.fd = fd
	l.addr = sockaddr
	l.network = network
	return l, nil
}
