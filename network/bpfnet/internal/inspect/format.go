package inspect

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/cofy-x/axern/network/bpfnet/internal/tcprog"
)

func rawStruct[T any](value T) any {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, value); err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return hex.EncodeToString(buf.Bytes())
}

func protoName(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return fmt.Sprintf("proto-%d", proto)
	}
}

func formatServiceKey(key tcprog.DataplaneServiceKey) any {
	return map[string]any{"protocol": protoName(key.Proto), "host_port": key.HostPort}
}

func formatServiceValue(value tcprog.DataplaneServiceValue) any {
	return map[string]any{"target_ip": ipv4FromUint32(value.TargetIp), "target_port": value.TargetPort}
}

func formatLocalAddrKey(key tcprog.DataplaneLocalAddrKey) any {
	return ipv4FromUint32(key.Addr)
}

func formatUplinkKey(key tcprog.DataplaneUplinkAddrKey) any {
	return key.Ifindex
}

func formatUplinkValue(value tcprog.DataplaneUplinkAddrValue) any {
	return ipv4FromUint32(value.Addr)
}

func formatNativeRouteKey(key tcprog.DataplaneNativeRouteKey) any {
	return fmt.Sprintf("%s/%d", ipv4FromUint32(key.Addr), key.Prefixlen)
}

func ipv4FromUint32(value uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, value)
	return ip.String()
}
