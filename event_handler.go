package fastudp

import "net"

type EventHandler interface {
	OnReaded([]byte, *net.UDPAddr)
	OnEventLoopClosed(err error)
}
