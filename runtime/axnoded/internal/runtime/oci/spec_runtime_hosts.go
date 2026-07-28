package oci

import (
	"bufio"
	"os"
	"strings"
)

func buildHostsFile(hostname string) string {
	lines := []string{
		"127.0.0.1 localhost",
		"::1 localhost ip6-localhost ip6-loopback",
	}
	if hostname != "" && hostname != "localhost" {
		lines = append(lines, "127.0.1.1 "+hostname)
	}
	lines = append(lines, hostDockerInternalHostEntries()...)
	return strings.Join(lines, "\n") + "\n"
}

func hostDockerInternalHostEntries() []string {
	content, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return nil
	}
	var entries []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "host.docker.internal") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}
