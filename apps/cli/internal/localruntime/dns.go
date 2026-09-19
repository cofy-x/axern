package localruntime

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
)

const localDNSNameserversEnv = "AXERN_LOCAL_DNS_NAMESERVERS"

var defaultLocalDNSResolverPaths = []string{
	"/etc/resolv.conf",
	"/run/systemd/resolve/resolv.conf",
}

func localDNSNameservers() ([]string, error) {
	return desiredLocalDNSNameservers(runtime.GOOS, os.Getenv(localDNSNameserversEnv), defaultLocalDNSResolverPaths)
}

func desiredLocalDNSNameservers(goos, override string, paths []string) ([]string, error) {
	if strings.TrimSpace(override) != "" {
		return discoverLocalDNSNameservers(override, nil)
	}
	// Docker Desktop owns a VM-local resolver that is more authoritative and
	// reachable from the Node container than the macOS host resolver set. On
	// native Linux, materialize the host's usable resolver snapshot so a local
	// stub such as systemd-resolved is never propagated into a nested sandbox.
	if goos != "linux" {
		return nil, nil
	}
	return discoverLocalDNSNameservers("", paths)
}

func discoverLocalDNSNameservers(override string, paths []string) ([]string, error) {
	if strings.TrimSpace(override) != "" {
		values := strings.Split(override, ",")
		nameservers := make([]string, 0, len(values))
		for _, value := range values {
			address, ok := usableDNSNameserver(value)
			if !ok {
				return nil, fmt.Errorf("%s contains unusable resolver %q", localDNSNameserversEnv, strings.TrimSpace(value))
			}
			nameservers = appendUnique(nameservers, address)
		}
		if len(nameservers) == 0 {
			return nil, fmt.Errorf("%s does not contain a usable resolver", localDNSNameserversEnv)
		}
		return nameservers, nil
	}

	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		var fileNameservers []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 || fields[0] != "nameserver" {
				continue
			}
			if address, ok := usableDNSNameserver(fields[1]); ok {
				fileNameservers = appendUnique(fileNameservers, address)
			}
		}
		_ = file.Close()
		if scanner.Err() != nil {
			continue
		}
		if len(fileNameservers) > 0 {
			return fileNameservers, nil
		}
	}
	return nil, fmt.Errorf("host resolver configuration has no non-loopback nameserver")
}

func usableDNSNameserver(value string) (string, bool) {
	address := net.ParseIP(strings.TrimSpace(value))
	if address == nil || address.IsLoopback() || address.IsUnspecified() {
		return "", false
	}
	return address.String(), true
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
