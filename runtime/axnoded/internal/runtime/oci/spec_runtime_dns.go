package oci

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
)

var (
	dockerExtServerPattern  = regexp.MustCompile(`\b[a-zA-Z_]+\(([0-9a-fA-F:.]+)\)`)
	defaultHostResolvPaths  = []string{"/etc/resolv.conf", "/run/systemd/resolve/resolv.conf"}
	defaultResolvConfOption = []string{"ndots:0"}
)

func buildResolvConf(config RuntimeDNSConfig) (string, error) {
	config = config.withDefaults()
	if len(config.Nameservers) > 0 {
		return buildResolvConfFromParts(config.Nameservers, config.SearchDomains, config.Options)
	}
	for _, path := range config.HostResolvConfPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		resolv, err := buildResolvConfFromContent(string(content), config.Options)
		if err == nil {
			return resolv, nil
		}
	}
	return "", fmt.Errorf("derive OCI runtime DNS config: no usable nameserver found")
}

func buildResolvConfFromContent(content string, defaultOptions []string) (string, error) {
	var nameservers []string
	var searchDomains []string
	var options []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			if len(fields) > 1 {
				nameservers = appendUsableNameserver(nameservers, fields[1])
			}
		case "search":
			searchDomains = append(searchDomains, fields[1:]...)
		case "options":
			options = append(options, fields[1:]...)
		default:
			if strings.HasPrefix(line, "#") {
				nameservers = appendDockerExtServers(nameservers, line)
			}
		}
	}
	if len(options) == 0 {
		options = append([]string(nil), defaultOptions...)
	}
	return buildResolvConfFromParts(nameservers, searchDomains, options)
}

func buildResolvConfFromParts(nameservers []string, searchDomains []string, options []string) (string, error) {
	nameservers = normalizeNameservers(nameservers)
	if len(nameservers) == 0 {
		return "", fmt.Errorf("build OCI runtime resolv.conf: no usable nameserver found")
	}
	var builder strings.Builder
	for _, nameserver := range nameservers {
		fmt.Fprintf(&builder, "nameserver %s\n", nameserver)
	}
	writeResolvConfList(&builder, "search", searchDomains)
	writeResolvConfList(&builder, "options", options)
	return builder.String(), nil
}

func writeResolvConfList(builder *strings.Builder, key string, values []string) {
	wroteKey := false
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimPrefix(value, key+" "))
		if value == "" {
			continue
		}
		if !wroteKey {
			builder.WriteString(key)
			wroteKey = true
		}
		builder.WriteByte(' ')
		builder.WriteString(value)
	}
	if wroteKey {
		builder.WriteByte('\n')
	}
}

func appendDockerExtServers(nameservers []string, line string) []string {
	for _, match := range dockerExtServerPattern.FindAllStringSubmatch(line, -1) {
		if len(match) > 1 {
			nameservers = appendUsableNameserver(nameservers, match[1])
		}
	}
	return nameservers
}

func appendUsableNameserver(nameservers []string, value string) []string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return nameservers
	}
	for _, existing := range nameservers {
		if existing == ip.String() {
			return nameservers
		}
	}
	return append(nameservers, ip.String())
}

func normalizeNameservers(values []string) []string {
	nameservers := make([]string, 0, len(values))
	for _, value := range values {
		nameservers = appendUsableNameserver(nameservers, value)
	}
	return nameservers
}
