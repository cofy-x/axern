package config

import (
	"fmt"
	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"strings"
	"time"
)

func validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.ControlTarget) == "" {
		return Config{}, fmt.Errorf("control-target is required")
	}
	if strings.TrimSpace(cfg.ControlEdgeAddress) == "" {
		return Config{}, fmt.Errorf("control-edge-address is required")
	}
	if strings.TrimSpace(cfg.ControlEdgeTLSCACert) == "" || strings.TrimSpace(cfg.ControlEdgeTLSCert) == "" || strings.TrimSpace(cfg.ControlEdgeTLSKey) == "" {
		return Config{}, fmt.Errorf("control-edge-tls-ca-cert, control-edge-tls-cert, and control-edge-tls-key are required")
	}
	if strings.TrimSpace(cfg.TunnelRelayTarget) == "" {
		return Config{}, fmt.Errorf("tunnel-relay-target is required")
	}
	if strings.TrimSpace(cfg.TunnelRelayTLSCACert) == "" {
		return Config{}, fmt.Errorf("tunnel-relay-tls-ca-cert is required")
	}
	if strings.TrimSpace(cfg.TLSCACert) == "" || strings.TrimSpace(cfg.WorkloadBundle) == "" {
		return Config{}, fmt.Errorf("tls-ca-cert and workload-bundle are required")
	}
	if _, err := (workloadtls.Identity{Cluster: cfg.WorkloadCluster, Role: "gatewayd"}).URI(); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.SSHAddress) == "" {
		cfg.SSHAddress = DefaultSSHAddress
	}
	if cfg.SSHEnabled {
		if strings.TrimSpace(cfg.SSHHostKey) == "" {
			return Config{}, fmt.Errorf("ssh-host-key is required when ssh-enabled is true")
		}
	}
	if cfg.ReadHeaderTimeout <= 0 {
		cfg.ReadHeaderTimeout = 5 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	if cfg.TerminalIdleTimeout <= 0 {
		cfg.TerminalIdleTimeout = 10 * time.Minute
	}
	if cfg.TerminalMaxDuration <= 0 {
		cfg.TerminalMaxDuration = 2 * time.Hour
	}
	if cfg.TerminalMaxDuration > 2*time.Hour {
		return Config{}, fmt.Errorf("terminal-max-duration must not exceed the two-hour allocation access grant limit")
	}
	if cfg.TerminalMaxMessageBytes <= 0 {
		cfg.TerminalMaxMessageBytes = 1 << 20
	}
	if cfg.AccessGrantRetryAttempts <= 0 {
		cfg.AccessGrantRetryAttempts = nodekernel.DefaultAccessGrantRetryAttempts
	}
	if cfg.AccessGrantRetryBaseDelay <= 0 {
		cfg.AccessGrantRetryBaseDelay = nodekernel.DefaultAccessGrantRetryBaseDelay
	}
	return cfg, nil
}
