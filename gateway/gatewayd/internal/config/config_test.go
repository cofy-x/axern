package config

import (
	"testing"
	"time"
)

func TestParseGatewayHardeningDefaults(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ServiceUpstreamTimeout != 30*time.Second {
		t.Fatalf("ServiceUpstreamTimeout = %s, want 30s", cfg.ServiceUpstreamTimeout)
	}
	if cfg.ServiceMaxRequestBodyBytes != 32<<20 {
		t.Fatalf("ServiceMaxRequestBodyBytes = %d, want 32MiB", cfg.ServiceMaxRequestBodyBytes)
	}
	if cfg.ServiceEndpointRetryAttempts != 4 {
		t.Fatalf("ServiceEndpointRetryAttempts = %d, want 4", cfg.ServiceEndpointRetryAttempts)
	}
	if cfg.ServiceEndpointQuarantineTTL != 30*time.Second {
		t.Fatalf("ServiceEndpointQuarantineTTL = %s, want 30s", cfg.ServiceEndpointQuarantineTTL)
	}
	if cfg.RouteCacheTTL != 3*time.Second || cfg.RouteCacheMaxEntries != 8192 {
		t.Fatalf("route cache config = ttl:%s max:%d, want 3s/8192", cfg.RouteCacheTTL, cfg.RouteCacheMaxEntries)
	}
	if cfg.TerminalIdleTimeout != 10*time.Minute {
		t.Fatalf("TerminalIdleTimeout = %s, want 10m", cfg.TerminalIdleTimeout)
	}
	if cfg.DashboardEnabled {
		t.Fatal("DashboardEnabled = true, want false")
	}
	if cfg.TunnelRelayTarget != DefaultTunnelRelayTarget {
		t.Fatalf("TunnelRelayTarget = %q, want %q", cfg.TunnelRelayTarget, DefaultTunnelRelayTarget)
	}
	if cfg.ControlEdgeAddress != DefaultControlEdgeAddress {
		t.Fatalf("ControlEdgeAddress = %q, want %q", cfg.ControlEdgeAddress, DefaultControlEdgeAddress)
	}
	if cfg.DashboardVendorDir != DefaultDashboardVendorDir {
		t.Fatalf("DashboardVendorDir = %q, want %q", cfg.DashboardVendorDir, DefaultDashboardVendorDir)
	}
}

func TestParseGatewayHardeningEnvAndFlags(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")
	t.Setenv("GATEWAYD_SERVICE_UPSTREAM_TIMEOUT", "7s")
	t.Setenv("GATEWAYD_SERVICE_MAX_REQUEST_BODY_BYTES", "1234")
	t.Setenv("GATEWAYD_SERVICE_ENDPOINT_RETRY_ATTEMPTS", "2")
	t.Setenv("GATEWAYD_SERVICE_ENDPOINT_QUARANTINE_TTL", "12s")
	t.Setenv("GATEWAYD_ROUTE_CACHE_TTL", "4s")
	t.Setenv("GATEWAYD_ROUTE_CACHE_MAX_ENTRIES", "2048")
	t.Setenv("GATEWAYD_DASHBOARD_ENABLED", "true")
	t.Setenv("GATEWAYD_DASHBOARD_VENDOR_DIR", "/tmp/gateway-dashboard-vendor")
	t.Setenv("GATEWAYD_CONTROL_EDGE_ADDRESS", "127.0.0.1:25001")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_CA_CERT", "edge-ca.crt")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_CERT", "edge.crt")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_KEY", "edge.key")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TARGET", "tunneld:24100")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TLS_CA_CERT", "relay-ca.crt")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TLS_SERVER_NAME", "tunneld")

	cfg, err := Parse([]string{
		"-service-upstream-timeout=9s",
		"-service-endpoint-retry-attempts=6",
		"-service-endpoint-quarantine-ttl=15s",
		"-route-cache-ttl=5s",
		"-route-cache-max-entries=4096",
		"-terminal-idle-timeout=11s",
		"-lease-retry-attempts=5",
		"-dashboard-enabled=false",
		"-dashboard-vendor-dir=/var/lib/gateway-dashboard",
		"-control-edge-address=127.0.0.1:25002",
		"-tunnel-relay-target=127.0.0.1:24100",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ServiceUpstreamTimeout != 9*time.Second {
		t.Fatalf("ServiceUpstreamTimeout = %s, want 9s", cfg.ServiceUpstreamTimeout)
	}
	if cfg.ServiceMaxRequestBodyBytes != 1234 {
		t.Fatalf("ServiceMaxRequestBodyBytes = %d, want env value", cfg.ServiceMaxRequestBodyBytes)
	}
	if cfg.ServiceEndpointRetryAttempts != 6 {
		t.Fatalf("ServiceEndpointRetryAttempts = %d, want flag value", cfg.ServiceEndpointRetryAttempts)
	}
	if cfg.ServiceEndpointQuarantineTTL != 15*time.Second {
		t.Fatalf("ServiceEndpointQuarantineTTL = %s, want flag value", cfg.ServiceEndpointQuarantineTTL)
	}
	if cfg.RouteCacheTTL != 5*time.Second || cfg.RouteCacheMaxEntries != 4096 {
		t.Fatalf("route cache config = ttl:%s max:%d, want flag values", cfg.RouteCacheTTL, cfg.RouteCacheMaxEntries)
	}
	if cfg.TerminalIdleTimeout != 11*time.Second {
		t.Fatalf("TerminalIdleTimeout = %s, want 11s", cfg.TerminalIdleTimeout)
	}
	if cfg.LeaseRetryAttempts != 5 {
		t.Fatalf("LeaseRetryAttempts = %d, want 5", cfg.LeaseRetryAttempts)
	}
	if cfg.DashboardEnabled {
		t.Fatal("DashboardEnabled = true, want flag override false")
	}
	if cfg.ControlEdgeAddress != "127.0.0.1:25002" {
		t.Fatalf("ControlEdgeAddress = %q, want flag value", cfg.ControlEdgeAddress)
	}
	if cfg.ControlEdgeTLSCACert != "edge-ca.crt" || cfg.ControlEdgeTLSCert != "edge.crt" || cfg.ControlEdgeTLSKey != "edge.key" {
		t.Fatalf("control edge tls config = %#v", cfg)
	}
	if cfg.TunnelRelayTarget != "127.0.0.1:24100" || cfg.TunnelRelayTLSCACert != "relay-ca.crt" || cfg.TunnelRelayTLSServerName != "tunneld" {
		t.Fatalf("tunnel relay config = %#v", cfg)
	}
	if cfg.DashboardVendorDir != "/var/lib/gateway-dashboard" {
		t.Fatalf("DashboardVendorDir = %q, want flag value", cfg.DashboardVendorDir)
	}
}

func TestParseRequiresControlEdgeTLS(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	if _, err := Parse([]string{
		"-control-edge-tls-ca-cert=",
	}); err == nil {
		t.Fatal("Parse() error = nil, want missing control edge TLS error")
	}
}

func TestParseRequiresTunnelRelayTLS(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	if _, err := Parse([]string{"-tunnel-relay-tls-ca-cert="}); err == nil {
		t.Fatal("Parse() error = nil, want missing tunnel relay TLS error")
	}
}

func TestParseDashboardEnabledFromEnv(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")
	t.Setenv("GATEWAYD_DASHBOARD_ENABLED", "true")
	t.Setenv("GATEWAYD_DASHBOARD_VENDOR_DIR", "")

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.DashboardEnabled {
		t.Fatal("DashboardEnabled = false, want true")
	}
	if cfg.DashboardVendorDir != DefaultDashboardVendorDir {
		t.Fatalf("DashboardVendorDir = %q, want default", cfg.DashboardVendorDir)
	}
}

func TestParseSSHDefaultsDisabled(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.SSHEnabled {
		t.Fatal("SSHEnabled = true, want false")
	}
	if cfg.SSHAddress != DefaultSSHAddress {
		t.Fatalf("SSHAddress = %q, want %q", cfg.SSHAddress, DefaultSSHAddress)
	}
}

func TestParseSSHEnabledRequiresKeyPaths(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	if _, err := Parse([]string{"-ssh-enabled=true"}); err == nil {
		t.Fatal("Parse() error = nil, want missing ssh key paths error")
	}
}

func TestParseSSHEnabledAcceptsKeyPaths(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	cfg, err := Parse([]string{
		"-ssh-enabled=true",
		"-ssh-address=127.0.0.1:2222",
		"-ssh-host-key=host.key",
		"-ssh-authorized-keys=authorized_keys",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.SSHEnabled || cfg.SSHAddress != "127.0.0.1:2222" || cfg.SSHHostKey != "host.key" || cfg.SSHAuthorizedKeys != "authorized_keys" {
		t.Fatalf("ssh config = %#v", cfg)
	}
}
