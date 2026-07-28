package resources

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func generateIP(ipRange string, maxNum uint32) (net.IP, net.IPMask, map[string]struct{}, error) {
	gateway, ipv4Net, err := net.ParseCIDR(ipRange)
	if err != nil {
		return net.IPv4zero, nil, nil, err
	}
	mask := binary.BigEndian.Uint32(ipv4Net.Mask)
	start := binary.BigEndian.Uint32(gateway.To4())
	finish := (start & mask) | (mask ^ 0xffffffff)

	if finish-start < maxNum {
		return net.IPv4zero, nil, nil, fmt.Errorf("ip range is too small, should be at least %d", maxNum)
	}

	ipset := make(map[string]struct{})
	for i := start; i < start+maxNum; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, i)
		if ip.String() == gateway.String() {
			continue
		}
		ipset[ip.String()] = struct{}{}
	}
	return gateway, ipv4Net.Mask, ipset, nil
}

func ipToVeth(ip string) (host, peer string) {
	parsedIP := net.ParseIP(ip)
	ipInHex := hex.EncodeToString(parsedIP.To4())
	return config.HostVethPrefix + ipInHex, config.PeerVethPrefix + ipInHex
}

func vethToIP(veth string) net.IP {
	if strings.HasPrefix(veth, config.HostVethPrefix) {
		veth = veth[len(config.HostVethPrefix):]
	} else if strings.HasPrefix(veth, config.PeerVethPrefix) {
		veth = veth[len(config.PeerVethPrefix):]
	}
	ip, _ := hex.DecodeString(veth)
	return ip
}
