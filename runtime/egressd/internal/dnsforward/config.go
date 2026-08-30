package dnsforward

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const DefaultDNSPort = 53

// ParseUpstreams accepts only explicit IP nameservers supplied by axnoded.
// It intentionally has no default and never consults host resolver state.
func ParseUpstreams(values []string) ([]netip.AddrPort, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one validated upstream nameserver is required")
	}
	seen := map[netip.AddrPort]struct{}{}
	out := make([]netip.AddrPort, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("upstream nameserver is required")
		}
		addrPort, err := parseUpstream(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[addrPort]; ok {
			continue
		}
		seen[addrPort] = struct{}{}
		out = append(out, addrPort)
	}
	return out, nil
}

func parseUpstream(value string) (netip.AddrPort, error) {
	if addr, err := netip.ParseAddr(value); err == nil {
		addr = addr.Unmap()
		if err := validateUpstreamAddress(value, addr); err != nil {
			return netip.AddrPort{}, err
		}
		return netip.AddrPortFrom(addr, DefaultDNSPort), nil
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid upstream nameserver %q: expected an IP or IP:port", value)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid upstream nameserver %q: hostnames are not allowed", value)
	}
	addr = addr.Unmap()
	if err := validateUpstreamAddress(value, addr); err != nil {
		return netip.AddrPort{}, err
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return netip.AddrPort{}, fmt.Errorf("invalid upstream nameserver %q: port must be 1..65535", value)
	}
	return netip.AddrPortFrom(addr, uint16(port)), nil
}

func validateUpstreamAddress(value string, addr netip.Addr) error {
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() {
		return fmt.Errorf("invalid upstream nameserver %q: address is not a usable node resolver", value)
	}
	return nil
}
