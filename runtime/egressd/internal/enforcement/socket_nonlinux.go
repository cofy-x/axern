//go:build !linux

package enforcement

import (
	"fmt"
	"net"
	"net/netip"
)

func originalDestination(net.Conn) (netip.AddrPort, error) {
	return netip.AddrPort{}, fmt.Errorf("transparent proxy requires Linux")
}
func dialUpstream(netip.AddrPort) (net.Conn, error) {
	return nil, fmt.Errorf("proxy dial requires Linux")
}

func listenTCP(port int) ([]net.Listener, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return []net.Listener{listener}, nil
}
