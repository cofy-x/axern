//go:build linux

package enforcement

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"syscall"

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

func dialMarked(destination netip.AddrPort) (net.Conn, error) {
	dialer := net.Dialer{Timeout: inspectTimeout, Control: func(_, _ string, raw syscall.RawConn) error {
		var result error
		if err := raw.Control(func(fd uintptr) { result = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, bypassMark) }); err != nil {
			return err
		}
		return result
	}}
	return dialer.DialContext(context.Background(), "tcp", destination.String())
}

func listenTransparentTCP(port int) (net.Listener, error) {
	config := net.ListenConfig{Control: transparentSocketControl}
	return config.Listen(context.Background(), "tcp6", fmt.Sprintf("[::]:%d", port))
}

func listenTransparentUDP(port int) (*net.UDPConn, error) {
	config := net.ListenConfig{Control: transparentSocketControl}
	packet, err := config.ListenPacket(context.Background(), "udp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		return nil, err
	}
	udp, ok := packet.(*net.UDPConn)
	if !ok {
		_ = packet.Close()
		return nil, fmt.Errorf("transparent DNS listener is not UDP")
	}
	return udp, nil
}

func transparentSocketControl(_, _ string, raw syscall.RawConn) error {
	var result error
	if err := raw.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
			result = err
			return
		}
		if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TRANSPARENT, 1); err != nil {
			result = err
			return
		}
		result = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TRANSPARENT, 1)
	}); err != nil {
		return err
	}
	return result
}
