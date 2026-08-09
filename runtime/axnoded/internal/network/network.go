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

	SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error

	CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error
}

// DNATRule is the backend-neutral desired state for one host-port mapping.
type DNATRule struct {
	Protocol   string
	HostPort   uint16
	TargetIP   string
	TargetPort uint16
}

// DNATReconciler is implemented by backends that persist DNAT intent outside
// the sandbox lifecycle store. Reconciliation removes orphaned mappings and
// ensures every live mapping is programmed after process recovery.
type DNATReconciler interface {
	ReconcileDNATRules([]DNATRule) error
}

// HealthProber reports dataplane facts without mutating host networking.
// Platform capabilities are published only from this verified state.
type HealthProber interface {
	ProbeHealth(ipRange string) (Health, error)
}

type Health struct {
	PortForwardingReady  bool
	NativeDataplaneReady bool
}

var NetworkManagers = map[string]NetworkManager{}

func Register(name string, manager NetworkManager) {
	NetworkManagers[name] = manager
}
