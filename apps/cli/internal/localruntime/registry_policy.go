package localruntime

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

const (
	internalRegistryHost          = "registry:5000"
	maxExternalInsecureRegistries = 16
)

func resolveExternalRegistryPolicy(existing []string, metadataExists bool, options UpOptions) ([]string, error) {
	if options.SetInsecureRegistries && options.ClearInsecureRegistries {
		return nil, fmt.Errorf("--insecure-registry and --clear-insecure-registries cannot be used together")
	}
	values := existing
	if !metadataExists || options.ClearInsecureRegistries {
		values = nil
	}
	if options.SetInsecureRegistries {
		values = options.ExternalInsecureRegistries
	}
	return normalizeExternalInsecureRegistries(values)
}

func normalizeExternalInsecureRegistries(values []string) ([]string, error) {
	if len(values) > maxExternalInsecureRegistries {
		return nil, fmt.Errorf("at most %d external insecure registries may be configured", maxExternalInsecureRegistries)
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		host, err := normalizeRegistryHost(value)
		if err != nil {
			return nil, err
		}
		if host == internalRegistryHost {
			return nil, fmt.Errorf("registry host %q is managed internally and cannot be configured as external", host)
		}
		unique[host] = struct{}{}
	}
	if len(unique) > maxExternalInsecureRegistries {
		return nil, fmt.Errorf("at most %d external insecure registries may be configured", maxExternalInsecureRegistries)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(unique))
	for host := range unique {
		result = append(result, host)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeRegistryHost(value string) (string, error) {
	original := value
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("insecure registry host is empty")
	}
	if value != original || strings.ContainsAny(value, "/@?#,\\") || strings.Contains(value, "://") {
		return "", fmt.Errorf("insecure registry %q must be a host or host:port without a scheme, path, credentials, query, fragment, or comma", original)
	}

	bracketed := strings.HasPrefix(value, "[")
	host, port, err := splitRegistryHost(value)
	if err != nil {
		return "", fmt.Errorf("invalid insecure registry %q: %w", original, err)
	}
	host = strings.ToLower(host)
	isIP := false
	if addr, parseErr := netip.ParseAddr(host); parseErr == nil {
		host = addr.String()
		isIP = true
	} else if err := validateDNSHost(host); err != nil {
		return "", fmt.Errorf("invalid insecure registry %q: %w", original, err)
	}
	if bracketed && (!isIP || !strings.Contains(host, ":")) {
		return "", fmt.Errorf("invalid insecure registry %q: brackets are only valid around an IPv6 address", original)
	}
	if port == "" {
		if !isIP && host != "localhost" && !strings.Contains(host, ".") {
			return "", fmt.Errorf("invalid insecure registry %q: a single-label registry host must include a port", original)
		}
		if strings.Contains(host, ":") {
			return "[" + host + "]", nil
		}
		return host, nil
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return "", fmt.Errorf("invalid insecure registry %q: port must be between 1 and 65535", original)
	}
	return net.JoinHostPort(host, strconv.FormatUint(portNumber, 10)), nil
}

func splitRegistryHost(value string) (string, string, error) {
	if strings.HasPrefix(value, "[") {
		if strings.HasSuffix(value, "]") {
			return strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), "", nil
		}
		host, port, err := net.SplitHostPort(value)
		return host, port, err
	}
	if strings.Count(value, ":") > 1 {
		return "", "", fmt.Errorf("IPv6 addresses must be enclosed in brackets")
	}
	if strings.Contains(value, ":") {
		host, port, ok := strings.Cut(value, ":")
		if !ok || host == "" || port == "" {
			return "", "", fmt.Errorf("host and port must both be present")
		}
		return host, port, nil
	}
	return value, "", nil
}

func validateDNSHost(host string) error {
	if host == "" || len(host) > 253 || strings.HasSuffix(host, ".") {
		return fmt.Errorf("host name is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("host name is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return fmt.Errorf("host name is invalid")
			}
		}
	}
	return nil
}

func effectiveInsecureRegistries(external []string) string {
	values := make([]string, 0, len(external)+1)
	values = append(values, internalRegistryHost)
	values = append(values, external...)
	return strings.Join(values, ",")
}

func registryNoProxyHosts(external []string) []string {
	result := make([]string, 0, len(external))
	seen := make(map[string]struct{}, len(external))
	for _, registry := range external {
		host := registry
		if parsed, _, err := net.SplitHostPort(registry); err == nil {
			host = parsed
		} else {
			host = strings.TrimPrefix(strings.TrimSuffix(registry, "]"), "[")
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result
}
