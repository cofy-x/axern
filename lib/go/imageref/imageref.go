package imageref

import "strings"

const DefaultRegistry = "index.docker.io"

func Normalize(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	return ref
}

func RegistryHost(ref string) string {
	ref = Normalize(ref)
	if ref == "" {
		return ""
	}
	if !strings.Contains(ref, "/") {
		return DefaultRegistry
	}
	first := ref
	if slash := strings.IndexByte(ref, '/'); slash >= 0 {
		first = ref[:slash]
	}
	if first == "localhost" || strings.Contains(first, ".") || strings.Contains(first, ":") {
		return first
	}
	return DefaultRegistry
}

func HostSetFromCSV(raw string) map[string]struct{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	values := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		host := NormalizeRegistryHost(item)
		if host == "" {
			continue
		}
		values[host] = struct{}{}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func NormalizeRegistryHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimSuffix(host, "/")
	return host
}

func UseHTTPFor(ref string, insecureRegistries map[string]struct{}) bool {
	if len(insecureRegistries) == 0 {
		return false
	}
	host := RegistryHost(ref)
	if host == "" {
		return false
	}
	_, ok := insecureRegistries[host]
	return ok
}
