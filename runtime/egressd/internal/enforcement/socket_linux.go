//go:build linux

package enforcement

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"

	"golang.org/x/sys/unix"
)

const (
	soOriginalDst = 80
)

func originalDestination(conn net.Conn) (destination netip.AddrPort, resultErr error) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return destination, fmt.Errorf("not a TCP connection")
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return destination, err
	}
	controlErr := raw.Control(func(fd uintptr) {
		var storage [128]byte
		length := uint32(len(storage))
		_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, uintptr(unix.SOL_IP), uintptr(soOriginalDst), uintptr(unsafePointer(&storage[0])), uintptr(unsafePointer(&length)), 0)
		if errno != 0 {
			_, _, errno = unix.Syscall6(unix.SYS_GETSOCKOPT, fd, uintptr(unix.SOL_IPV6), uintptr(soOriginalDst), uintptr(unsafePointer(&storage[0])), uintptr(unsafePointer(&length)), 0)
		}
		if errno != 0 {
			resultErr = errno
			return
		}
		family := binary.LittleEndian.Uint16(storage[:2])
		port := binary.BigEndian.Uint16(storage[2:4])
		switch family {
		case unix.AF_INET:
			destination = netip.AddrPortFrom(netip.AddrFrom4([4]byte(storage[4:8])), port)
		case unix.AF_INET6:
			var address [16]byte
			copy(address[:], storage[8:24])
			destination = netip.AddrPortFrom(netip.AddrFrom16(address), port)
		default:
			resultErr = fmt.Errorf("unsupported original destination family %d", family)
		}
	})
	if controlErr != nil {
		return destination, controlErr
	}
	if resultErr != nil {
		if local, ok := conn.LocalAddr().(*net.TCPAddr); ok && local.IP != nil && local.Port > 0 {
			return local.AddrPort(), nil
		}
		return destination, resultErr
	}
	if !destination.IsValid() {
		return destination, fmt.Errorf("original destination is unavailable")
	}
	return destination, nil
}

func dialUpstream(destination netip.AddrPort) (net.Conn, error) {
	dialer := net.Dialer{Timeout: inspectTimeout}
	return dialer.DialContext(context.Background(), "tcp", destination.String())
}

func listenTCP(port int) ([]net.Listener, error) {
	var listeners []net.Listener
	for _, endpoint := range []struct {
		network string
		address string
	}{{"tcp4", fmt.Sprintf("0.0.0.0:%d", port)}, {"tcp6", fmt.Sprintf("[::]:%d", port)}} {
		listener, err := net.Listen(endpoint.network, endpoint.address)
		if err != nil {
			closeAll(listeners)
			return nil, err
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}
