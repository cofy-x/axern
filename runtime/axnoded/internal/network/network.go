package network

import (
	"net"
)

type NetworkManager interface {
	SetupSNATRules(ipRange string) error

	CleanupSNATRules(ipRange string) error

	// SetupNetworkRulesForActivating updates any backend-specific rules tied to
	// sandbox activation. The iptables backend currently treats this as a no-op.
	SetupNetworkRulesForActivating(ip net.IP, envID string) error

	// CleanupNetworkRulesForActivating rolls back any activation-specific rules.
	// The iptables backend currently treats this as a no-op.
	CleanupNetworkRulesForActivating(ip net.IP) error
}

// HealthProber reports dataplane facts without mutating host networking.
// Platform capabilities are published only from this verified state.
type HealthProber interface {
	ProbeHealth(ipRange string) (Health, error)
}

type Health struct {
	NativeDataplaneReady bool
}

var NetworkManagers = map[string]NetworkManager{}

func Register(name string, manager NetworkManager) {
	NetworkManagers[name] = manager
}
