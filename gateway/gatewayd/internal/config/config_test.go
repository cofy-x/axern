package config

import (
	"testing"
	"time"
)

func TestParseRejectsTerminalDurationBeyondGrantLimit(t *testing.T) {
	if _, err := Parse([]string{"-terminal-max-duration=3h"}); err == nil {
		t.Fatal("terminal duration beyond grant limit accepted")
	}
}

func TestParseGatewayHardeningDefaults(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")

	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.TerminalIdleTimeout != 10*time.Minute {
		t.Fatalf("TerminalIdleTimeout = %s, want 10m", cfg.TerminalIdleTimeout)
	}
	if cfg.TunnelRelayTarget != DefaultTunnelRelayTarget {
		t.Fatalf("TunnelRelayTarget = %q, want %q", cfg.TunnelRelayTarget, DefaultTunnelRelayTarget)
	}
	if cfg.ControlEdgeAddress != DefaultControlEdgeAddress {
		t.Fatalf("ControlEdgeAddress = %q, want %q", cfg.ControlEdgeAddress, DefaultControlEdgeAddress)
	}
}

func TestParseGatewayHardeningEnvAndFlags(t *testing.T) {
	t.Setenv("GATEWAYD_CONTROL_TARGET", "127.0.0.1:24000")
	t.Setenv("GATEWAYD_TLS_CA_CERT", "ca.crt")
	t.Setenv("GATEWAYD_TLS_CERT", "client.crt")
	t.Setenv("GATEWAYD_TLS_KEY", "client.key")
	t.Setenv("GATEWAYD_CONTROL_EDGE_ADDRESS", "127.0.0.1:25001")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_CA_CERT", "edge-ca.crt")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_CERT", "edge.crt")
	t.Setenv("GATEWAYD_CONTROL_EDGE_TLS_KEY", "edge.key")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TARGET", "tunneld:24100")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TLS_CA_CERT", "relay-ca.crt")
	t.Setenv("GATEWAYD_TUNNEL_RELAY_TLS_SERVER_NAME", "tunneld")

	cfg, err := Parse([]string{
		"-terminal-idle-timeout=11s",
		"-access-grant-retry-attempts=5",
		"-control-edge-address=127.0.0.1:25002",
		"-tunnel-relay-target=127.0.0.1:24100",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.TerminalIdleTimeout != 11*time.Second {
		t.Fatalf("TerminalIdleTimeout = %s, want 11s", cfg.TerminalIdleTimeout)
	}
	if cfg.AccessGrantRetryAttempts != 5 {
		t.Fatalf("AccessGrantRetryAttempts = %d, want 5", cfg.AccessGrantRetryAttempts)
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
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !cfg.SSHEnabled || cfg.SSHAddress != "127.0.0.1:2222" || cfg.SSHHostKey != "host.key" {
		t.Fatalf("ssh config = %#v", cfg)
	}
}
