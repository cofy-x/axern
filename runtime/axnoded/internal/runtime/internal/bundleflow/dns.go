package bundleflow

import (
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func DNSConfigFromRuntimeConfig(config config.RuntimeDNSConfig) runtimeoci.RuntimeDNSConfig {
	return runtimeoci.RuntimeDNSConfig{
		Nameservers:   append([]string(nil), config.Nameservers...),
		SearchDomains: append([]string(nil), config.SearchDomains...),
		Options:       append([]string(nil), config.Options...),
	}
}
