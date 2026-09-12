package imagefsd

import (
	"encoding/json"
	"fmt"
	"os"
)

// ProxyConfig maps to nydus_api::ProxyConfig
type ProxyConfig struct {
	Url               string `json:"url,omitempty"`
	PingUrl           string `json:"ping_url,omitempty"`
	Fallback          bool   `json:"fallback"`
	CheckInterval     uint64 `json:"check_interval,omitempty"`
	CheckPauseElapsed uint64 `json:"check_pause_elapsed,omitempty"`
}

// RegistryConfig maps to nydus_api::RegistryConfig
type RegistryConfig struct {
	Scheme             string       `json:"scheme,omitempty"`
	Host               string       `json:"host,omitempty"`
	Repo               string       `json:"repo,omitempty"`
	Auth               string       `json:"auth,omitempty"`
	SkipVerify         bool         `json:"skip_verify,omitempty"`
	Timeout            uint32       `json:"timeout,omitempty"`
	ConnectTimeout     uint32       `json:"connect_timeout,omitempty"`
	RetryLimit         uint8        `json:"retry_limit,omitempty"`
	CaCertFiles        []string     `json:"ca_cert_files,omitempty"`
	RegistryToken      string       `json:"registry_token,omitempty"`
	BlobUrlScheme      string       `json:"blob_url_scheme,omitempty"`
	BlobRedirectedHost string       `json:"blob_redirected_host,omitempty"`
	Proxy              *ProxyConfig `json:"proxy,omitempty"`
}

// BackendConfig maps to nydus_api::BackendConfigV2
type BackendConfig struct {
	BackendType string          `json:"type"` // "registry"
	Registry    *RegistryConfig `json:"registry,omitempty"`
}

// DeepCopy returns a deep copy of BackendConfig, duplicating all pointer fields
func (cfg *BackendConfig) DeepCopy() BackendConfig {
	out := *cfg
	if cfg.Registry != nil {
		regCopy := *cfg.Registry
		regCopy.CaCertFiles = append([]string(nil), cfg.Registry.CaCertFiles...)
		if cfg.Registry.Proxy != nil {
			proxyCopy := *cfg.Registry.Proxy
			regCopy.Proxy = &proxyCopy
		}
		out.Registry = &regCopy
	}
	return out
}

func (cfg *BackendConfig) LoadTemplate(templatePath string) error {
	file, err := os.Open(templatePath)
	if err != nil {
		return fmt.Errorf("failed to open backend config template file, err= %w", err)
	}
	defer file.Close()
	if err = json.NewDecoder(file).Decode(cfg); err != nil {
		return fmt.Errorf("invalid json fmt file, path = %s, err = %w", templatePath, err)
	}
	return nil
}
