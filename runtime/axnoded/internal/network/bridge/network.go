package bridge

import (
	"fmt"
	"net"
	"strconv"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/coreos/go-iptables/iptables"
)

type BridgeNetworkManager struct{}

func (BridgeNetworkManager) ProbeHealth(ipRange string) (networkmanager.Health, error) {
	ipt, err := iptables.New()
	if err != nil {
		return networkmanager.Health{}, err
	}
	exists, err := ipt.Exists("nat", "POSTROUTING", "-s", ipRange, "-j", "MASQUERADE")
	if err != nil {
		return networkmanager.Health{}, err
	}
	if !exists {
		return networkmanager.Health{}, fmt.Errorf("bridge SNAT rule is missing")
	}
	return networkmanager.Health{PortForwardingReady: true, NativeDataplaneReady: true}, nil
}

// SetupSNATRules implements resourcemanager.NetworkManager.
func (BridgeNetworkManager) SetupSNATRules(ipRange string) error {
	// add follow iptable rule: iptables -t nat -A POSTROUTING -s 172.17.0.0/16 -j MASQUERADE
	ipt, err := iptables.New()
	if err != nil {
		return err
	}
	// check if rule exists.
	if exists, err := ipt.Exists("nat", "POSTROUTING", "-s", ipRange, "-j", "MASQUERADE"); err != nil {
		return err
	} else if exists {
		return nil
	}

	// create rule.
	return ipt.Append("nat", "POSTROUTING", "-s", ipRange, "-j", "MASQUERADE")
}

// CleanupSNATRules implements resourcemanager.NetworkManager.
func (BridgeNetworkManager) CleanupSNATRules(ipRange string) error {
	// clean iptable rule if exists.
	ipt, err := iptables.New()
	if err != nil {
		return err
	}
	// check if rule exists.
	if exists, err := ipt.Exists("nat", "POSTROUTING", "-s", ipRange, "-j", "MASQUERADE"); err != nil {
		return err
	} else if !exists {
		return nil
	}

	// delete rule.
	return ipt.Delete("nat", "POSTROUTING", "-s", ipRange, "-j", "MASQUERADE")
}

// SetupNetworkRulesForActivating is a no-op for the iptables backend.
func (BridgeNetworkManager) SetupNetworkRulesForActivating(ip net.IP, envID string) error {
	return nil
}

// CleanupNetworkRulesForActivating is a no-op for the iptables backend.
func (BridgeNetworkManager) CleanupNetworkRulesForActivating(ip net.IP) error {
	return nil
}

// SetupDNATRule implements networkmanager.NetworkManager.
func (BridgeNetworkManager) SetupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	return setupDNATRule(protocol, dstPort, targetIP, targetPort, true, true)
}

// SetupDNATCompatRule installs only the localhost OUTPUT + hairpin rules used
// by the ebpf backend when ingress TCP DNAT is handled in tc.
func (BridgeNetworkManager) SetupDNATCompatRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	return setupDNATRule(protocol, dstPort, targetIP, targetPort, false, true)
}

func setupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16, includeIngress bool, includeLocalhostCompat bool) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}

	dstPortStr := strconv.FormatUint(uint64(dstPort), 10)
	targetPortStr := strconv.FormatUint(uint64(targetPort), 10)
	toDest := fmt.Sprintf("%s:%s", targetIP, targetPortStr)

	if includeIngress {
		// iptables -t nat -A PREROUTING -p <proto> --dport <dstPort> -j DNAT --to-destination <targetIP>:<targetPort>
		if err := ipt.AppendUnique("nat", "PREROUTING", "-p", protocol, "--dport", dstPortStr, "-j", "DNAT", "--to-destination", toDest); err != nil {
			return fmt.Errorf("failed to add PREROUTING DNAT rule: %v", err)
		}

		// iptables -A FORWARD -p <proto> -d <targetIP> --dport <targetPort> -j ACCEPT
		if err := ipt.AppendUnique("filter", "FORWARD", "-p", protocol, "-d", targetIP, "--dport", targetPortStr, "-j", "ACCEPT"); err != nil {
			ipt.Delete("nat", "PREROUTING", "-p", protocol, "--dport", dstPortStr, "-j", "DNAT", "--to-destination", toDest)
			return fmt.Errorf("failed to add FORWARD rule: %v", err)
		}
	}

	if includeLocalhostCompat {
		// Match only host-local destinations. A port-only OUTPUT rule also
		// rewrites direct traffic to a sandbox that happens to use hostPort.
		outputRule := dnatOutputRule(protocol, dstPortStr, toDest)
		if err := ipt.AppendUnique("nat", "OUTPUT", outputRule...); err != nil {
			if includeIngress {
				ipt.Delete("filter", "FORWARD", "-p", protocol, "-d", targetIP, "--dport", targetPortStr, "-j", "ACCEPT")
				ipt.Delete("nat", "PREROUTING", "-p", protocol, "--dport", dstPortStr, "-j", "DNAT", "--to-destination", toDest)
			}
			return fmt.Errorf("failed to add OUTPUT DNAT rule: %v", err)
		}

		// Localhost traffic needs hairpin masquerade so replies can route back to
		// the host instead of trying to return directly to 127.0.0.1.
		if err := ipt.AppendUnique("nat", "POSTROUTING", "-p", protocol, "-s", "127.0.0.1/32", "-d", targetIP, "--dport", targetPortStr, "-j", "MASQUERADE"); err != nil {
			ipt.Delete("nat", "OUTPUT", outputRule...)
			if includeIngress {
				ipt.Delete("filter", "FORWARD", "-p", protocol, "-d", targetIP, "--dport", targetPortStr, "-j", "ACCEPT")
				ipt.Delete("nat", "PREROUTING", "-p", protocol, "--dport", dstPortStr, "-j", "DNAT", "--to-destination", toDest)
			}
			return fmt.Errorf("failed to add hairpin POSTROUTING MASQUERADE rule: %v", err)
		}
	}

	return nil
}

// CleanupDNATRule implements networkmanager.NetworkManager.
func (BridgeNetworkManager) CleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	return cleanupDNATRule(protocol, dstPort, targetIP, targetPort, true, true)
}

// CleanupDNATCompatRule removes only the localhost OUTPUT + hairpin rules used
// by the ebpf backend compatibility path.
func (BridgeNetworkManager) CleanupDNATCompatRule(protocol string, dstPort uint16, targetIP string, targetPort uint16) error {
	return cleanupDNATRule(protocol, dstPort, targetIP, targetPort, false, true)
}

func cleanupDNATRule(protocol string, dstPort uint16, targetIP string, targetPort uint16, includeIngress bool, includeLocalhostCompat bool) error {
	ipt, err := iptables.New()
	if err != nil {
		return err
	}

	dstPortStr := strconv.FormatUint(uint64(dstPort), 10)
	targetPortStr := strconv.FormatUint(uint64(targetPort), 10)
	toDest := fmt.Sprintf("%s:%s", targetIP, targetPortStr)

	// best-effort: remove both rules, report first error
	var firstErr error

	if includeIngress {
		if err := ipt.DeleteIfExists("nat", "PREROUTING", "-p", protocol, "--dport", dstPortStr, "-j", "DNAT", "--to-destination", toDest); err != nil {
			firstErr = fmt.Errorf("failed to delete PREROUTING DNAT rule: %v", err)
		}
		if err := ipt.DeleteIfExists("filter", "FORWARD", "-p", protocol, "-d", targetIP, "--dport", targetPortStr, "-j", "ACCEPT"); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to delete FORWARD rule: %v", err)
		}
	}

	if includeLocalhostCompat {
		if err := ipt.DeleteIfExists("nat", "OUTPUT", dnatOutputRule(protocol, dstPortStr, toDest)...); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to delete OUTPUT DNAT rule: %v", err)
		}
		if err := ipt.DeleteIfExists("nat", "POSTROUTING", "-p", protocol, "-s", "127.0.0.1/32", "-d", targetIP, "--dport", targetPortStr, "-j", "MASQUERADE"); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to delete hairpin POSTROUTING MASQUERADE rule: %v", err)
		}
	}

	return firstErr
}

func dnatOutputRule(protocol, dstPort, destination string) []string {
	return []string{
		"-p", protocol,
		"-m", "addrtype", "--dst-type", "LOCAL",
		"--dport", dstPort,
		"-j", "DNAT", "--to-destination", destination,
	}
}

func init() {
	networkmanager.Register(config.NatBackendIptables, &BridgeNetworkManager{})
}
