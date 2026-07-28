package diagnostic

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type PortSnapshot struct {
	Ports []Port `json:"ports"`
}

type Port struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	State    string `json:"state,omitempty"`
}

func Ports() PortSnapshot {
	ports := make([]Port, 0)
	for _, source := range []struct {
		path     string
		protocol string
	}{
		{"/proc/net/tcp", "tcp"},
		{"/proc/net/tcp6", "tcp6"},
		{"/proc/net/udp", "udp"},
		{"/proc/net/udp6", "udp6"},
	} {
		ports = append(ports, parseProcNet(source.path, source.protocol)...)
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Protocol != ports[j].Protocol {
			return ports[i].Protocol < ports[j].Protocol
		}
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Address < ports[j].Address
	})
	return PortSnapshot{Ports: ports}
}

func parseProcNet(path string, protocol string) []Port {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Port
	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		address, port, ok := parseProcNetLocalAddress(fields[1], strings.HasSuffix(protocol, "6"))
		if !ok {
			continue
		}
		state := fields[3]
		if strings.HasPrefix(protocol, "tcp") && state != "0A" {
			continue
		}
		out = append(out, Port{Protocol: protocol, Address: address, Port: port, State: state})
	}
	return out
}

func parseProcNetLocalAddress(value string, ipv6 bool) (string, int, bool) {
	hostHex, portHex, ok := strings.Cut(value, ":")
	if !ok {
		return "", 0, false
	}
	port64, err := strconv.ParseInt(portHex, 16, 32)
	if err != nil {
		return "", 0, false
	}
	if ipv6 {
		return hostHex, int(port64), true
	}
	if len(hostHex) != 8 {
		return hostHex, int(port64), true
	}
	parts := make([]string, 0, 4)
	for i := 6; i >= 0; i -= 2 {
		part, err := strconv.ParseInt(hostHex[i:i+2], 16, 32)
		if err != nil {
			return "", 0, false
		}
		parts = append(parts, fmt.Sprint(part))
	}
	return strings.Join(parts, "."), int(port64), true
}
