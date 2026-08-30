package localruntime

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

const localDNSNameserversEnv = "AXERN_LOCAL_DNS_NAMESERVERS"

var defaultLocalDNSResolverPaths = []string{
	"/etc/resolv.conf",
	"/run/systemd/resolve/resolv.conf",
}

func localDNSNameservers() ([]string, error) {
	override := os.Getenv(localDNSNameserversEnv)
	if strings.TrimSpace(override) == "" {
		return nil, nil
	}
	return discoverLocalDNSNameservers(override, nil)
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

	var nameservers []string
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
		for _, address := range fileNameservers {
			nameservers = appendUnique(nameservers, address)
		}
	}
	if len(nameservers) == 0 {
		return nil, fmt.Errorf("host resolver configuration has no non-loopback nameserver")
	}
	return nameservers, nil
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
