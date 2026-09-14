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
