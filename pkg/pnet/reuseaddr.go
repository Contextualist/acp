package pnet

// Adapted from libp2p/go-reuseport

import (
	"context"
	"fmt"
	"net"
)

var listenConfig = net.ListenConfig{Control: control}

func Listen(ctx context.Context, network, address string) (net.Listener, error) {
	return listenConfig.Listen(ctx, network, address)
}

func ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return listenConfig.ListenPacket(ctx, network, address)
}

func DialContext(ctx context.Context, network, laddr, raddr string) (net.Conn, error) {
	nla, err := resolveAddr(network, laddr)
	if err != nil {
		return nil, fmt.Errorf("dial failed to resolve laddr %v: %w", laddr, err)
	}
	d := net.Dialer{
		Control:   control,
		LocalAddr: nla,
	}
	return d.DialContext(ctx, network, raddr)
}

func resolveAddr(network, address string) (net.Addr, error) {
	switch network {
	default:
		return nil, net.UnknownNetworkError(network)
	case "ip", "ip4", "ip6":
		return net.ResolveIPAddr(network, address)
	case "tcp", "tcp4", "tcp6":
		return net.ResolveTCPAddr(network, address)
	case "udp", "udp4", "udp6":
		return net.ResolveUDPAddr(network, address)
	case "unix", "unixgram", "unixpacket":
		return net.ResolveUnixAddr(network, address)
	}
}
