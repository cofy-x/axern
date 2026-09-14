package bridge

import (
	"fmt"
	"net"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/coreos/go-iptables/iptables"
)

type BridgeNetworkManager struct{}

func (BridgeNetworkManager) ProbeHealth(ipRange string) (networkmanager.Health, error) {
	ipt, err := iptablesForCIDR(ipRange)
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
	return networkmanager.Health{NativeDataplaneReady: true}, nil
}

// SetupSNATRules implements resourcemanager.NetworkManager.
func (BridgeNetworkManager) SetupSNATRules(ipRange string) error {
	// add follow iptable rule: iptables -t nat -A POSTROUTING -s 172.17.0.0/16 -j MASQUERADE
	ipt, err := iptablesForCIDR(ipRange)
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
	ipt, err := iptablesForCIDR(ipRange)
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

func iptablesForCIDR(cidr string) (*iptables.IPTables, error) {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse network CIDR %q: %w", cidr, err)
	}
	ipt, _, err := iptablesForAddress(ip.String())
	return ipt, err
}

func iptablesForAddress(address string) (*iptables.IPTables, bool, error) {
	ip := net.ParseIP(address)
	if ip == nil {
		return nil, false, fmt.Errorf("parse network address %q", address)
	}
	if ip.To4() != nil {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		return ipt, false, err
	}
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	return ipt, true, err
}

func init() {
	networkmanager.Register(config.NatBackendIptables, &BridgeNetworkManager{})
}
