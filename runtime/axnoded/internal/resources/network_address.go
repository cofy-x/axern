package resources

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

const ipv6VethSuffixBytes = 6

func generateIP(ipRange string, maxNum uint32) (net.IP, net.IPMask, map[string]struct{}, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(ipRange))
	if err != nil {
		return nil, nil, nil, err
	}
	gateway := prefix.Addr().Unmap()
	if !gateway.Is4() && !gateway.Is6() {
		return nil, nil, nil, fmt.Errorf("ip range has unsupported address family: %s", ipRange)
	}
	prefix = netip.PrefixFrom(gateway, prefix.Bits())

	ipset := make(map[string]struct{}, maxNum)
	for candidate := gateway.Next(); candidate.IsValid() && prefix.Contains(candidate) && uint32(len(ipset)) < maxNum; candidate = candidate.Next() {
		ipset[candidate.String()] = struct{}{}
	}
	if uint32(len(ipset)) < maxNum {
		return nil, nil, nil, fmt.Errorf("ip range is too small, should provide at least %d sandbox addresses", maxNum)
	}

	bits := 128
	if gateway.Is4() {
		bits = 32
	}
	return net.IP(gateway.AsSlice()), net.CIDRMask(prefix.Bits(), bits), ipset, nil
}

func ipToVeth(ip string) (host, peer string) {
	parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return config.HostVethPrefix, config.PeerVethPrefix
	}
	parsed = parsed.Unmap()
	bytes := parsed.AsSlice()
	if parsed.Is6() {
		bytes = bytes[len(bytes)-ipv6VethSuffixBytes:]
	}
	suffix := hex.EncodeToString(bytes)
	return config.HostVethPrefix + suffix, config.PeerVethPrefix + suffix
}

func vethToIP(veth string, ipRange ...string) net.IP {
	suffix := strings.TrimPrefix(strings.TrimPrefix(veth, config.HostVethPrefix), config.PeerVethPrefix)
	decoded, err := hex.DecodeString(suffix)
	if err != nil {
		return nil
	}
	if len(decoded) == net.IPv4len {
		return net.IP(decoded)
	}
	if len(decoded) != ipv6VethSuffixBytes || len(ipRange) == 0 {
		return nil
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(ipRange[0]))
	if err != nil || !prefix.Addr().Is6() {
		return nil
	}
	bytes := prefix.Masked().Addr().As16()
	copy(bytes[len(bytes)-ipv6VethSuffixBytes:], decoded)
	return net.IP(bytes[:])
}
