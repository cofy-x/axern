package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/pelletier/go-toml"
)

const (
	defaultProbeTimeout = 15 * time.Second
	defaultConfigPath   = "/tmp/axnoded-node-config.toml"
	resolverPort        = "53"
)

type result struct {
	Status                  string `json:"status"`
	Code                    string `json:"code"`
	EffectiveResolverCount  int64  `json:"effective_resolver_count"`
	SuccessfulResolverCount int64  `json:"successful_resolver_count"`
}

type resolverLookup func(context.Context, string, string, time.Duration) bool

func main() {
	timeout := defaultProbeTimeout
	if value, err := time.ParseDuration(strings.TrimSpace(os.Getenv("AXERN_DNS_PROBE_TIMEOUT"))); err == nil && value > 0 {
		timeout = value
	}
	configPath := strings.TrimSpace(os.Getenv("AXERN_DNS_PROBE_CONFIG"))
	if configPath == "" {
		configPath = defaultConfigPath
	}
	resolvers, _ := effectiveResolvers(configPath)
	value := runProbe(context.Background(), resolvers, os.Getenv("AXERN_DNS_PROBE_NAME"), timeout, lookupResolver)
	_ = json.NewEncoder(os.Stdout).Encode(value)
}

func effectiveResolvers(configPath string) ([]string, error) {
	cfg := config.DefaultConfig()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read axnoded config: %w", err)
	}
	if err := toml.NewDecoder(bytes.NewReader(data)).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode axnoded config: %w", err)
	}
	dns := cfg.PluginConfig.RuntimeConfig.DNS
	return runtimeoci.ResolveRuntimeDNSNameservers(runtimeoci.RuntimeDNSConfig{
		Nameservers: append([]string(nil), dns.Nameservers...), SearchDomains: append([]string(nil), dns.SearchDomains...), Options: append([]string(nil), dns.Options...),
	})
}

func runProbe(ctx context.Context, resolvers []string, queryName string, timeout time.Duration, lookup resolverLookup) result {
	resolvers = parseResolvers(strings.Join(resolvers, ","))
	value := result{Status: "fail", Code: "runtime_dns_node_unreachable", EffectiveResolverCount: int64(len(resolvers))}
	if len(resolvers) == 0 || !validQueryName(queryName) || timeout <= 0 {
		return value
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan bool, len(resolvers))
	for _, resolver := range resolvers {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			results <- lookup(ctx, net.JoinHostPort(address, resolverPort), queryName, timeout)
		}(resolver)
	}
	wg.Wait()
	close(results)
	for ok := range results {
		if ok {
			value.SuccessfulResolverCount++
		}
	}
	if value.SuccessfulResolverCount > 0 {
		if value.SuccessfulResolverCount == value.EffectiveResolverCount {
			value.Status = "pass"
			value.Code = "runtime_dns_node_reachable"
		} else {
			value.Status = "warn"
			value.Code = "runtime_dns_node_partial"
		}
	}
	return value
}

func parseResolvers(raw string) []string {
	seen := map[netip.Addr]struct{}{}
	var result []string
	for _, item := range strings.Split(raw, ",") {
		address, err := netip.ParseAddr(strings.TrimSpace(item))
		if err != nil || address.IsUnspecified() || address.IsLoopback() {
			continue
		}
		address = address.Unmap()
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address.String())
	}
	return result
}

func validQueryName(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func lookupResolver(ctx context.Context, resolverAddress, queryName string, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, resolverAddress)
		},
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", queryName)
	if err != nil {
		return false
	}
	for _, address := range addresses {
		if address.Is4() || address.Is6() {
			return true
		}
	}
	return false
}
