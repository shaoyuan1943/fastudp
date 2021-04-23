package netudp

import "strings"

func IsUDP(network string) bool {
	switch strings.ToLower(network) {
	case "udp", "udp4", "udp6":
		return true
	}

	return false
}
