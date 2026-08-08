package oci

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func buildHostsFile(hostname string, sandboxIP net.IP) string {
	lines := []string{
		"127.0.0.1 localhost",
		"::1 localhost ip6-localhost ip6-loopback",
	}
	if hostname != "" && hostname != "localhost" && sandboxIP != nil {
		lines = append(lines, sandboxIP.String()+" "+hostname)
	}
	lines = append(lines, hostDockerInternalHostEntries()...)
	return strings.Join(lines, "\n") + "\n"
}

func sandboxIPv4FromSpec(ociSpec *spec.Spec) (net.IP, error) {
	if ociSpec == nil {
		return nil, fmt.Errorf("sandbox network IPv4 is required for /etc/hosts")
	}
	annotationKey := resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName)
	raw := strings.TrimSpace(ociSpec.Annotations[annotationKey])
	networkResource := &resourcemanager.NetResource{}
	if raw == "" || networkResource.FromString(raw) != nil || networkResource.Ip.To4() == nil {
		return nil, fmt.Errorf("sandbox network IPv4 is required for /etc/hosts")
	}
	return networkResource.Ip.To4(), nil
}

func hostDockerInternalHostEntries() []string {
	content, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return nil
	}
	return parseHostDockerInternalEntries(string(content))
}

func parseHostDockerInternalEntries(content string) []string {
	var entries []string
	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}
		found := false
		for _, alias := range fields[1:] {
			if strings.HasPrefix(alias, "#") {
				break
			}
			if alias == "host.docker.internal" {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		entry := ip.String() + " host.docker.internal"
		if _, exists := seen[entry]; exists {
			continue
		}
		seen[entry] = struct{}{}
		entries = append(entries, entry)
	}
	return entries
}
