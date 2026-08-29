//go:build !linux

package enforcement

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

func originalDestination(net.Conn) (netip.AddrPort, error) {
	return netip.AddrPort{}, fmt.Errorf("transparent proxy requires Linux")
}
func dialMarked(netip.AddrPort) (net.Conn, error) {
	return nil, fmt.Errorf("marked proxy dial requires Linux")
}

func listenTransparentTCP(port int) ([]net.Listener, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return []net.Listener{listener}, nil
}

func listenTransparentUDP(port int) ([]*net.UDPConn, error) {
	var config net.ListenConfig
	packet, err := config.ListenPacket(context.Background(), "udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return []*net.UDPConn{packet.(*net.UDPConn)}, nil
}
