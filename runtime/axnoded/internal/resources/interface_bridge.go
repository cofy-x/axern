package resources

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func prefixLength(mask net.IPMask) int {
	ones, _ := mask.Size()
	return ones
}

func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s failed: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return nil
}

func ensureIPv4Forwarding(bridge string) error {
	sysctls := map[string]string{
		"/proc/sys/net/ipv4/ip_forward":                  "1",
		"/proc/sys/net/ipv4/conf/all/forwarding":         "1",
		"/proc/sys/net/ipv4/conf/all/route_localnet":     "1",
		"/proc/sys/net/ipv4/conf/default/route_localnet": "1",
	}
	for path, value := range sysctls {
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			return fmt.Errorf("set %s=%s failed: %w", path, value, err)
		}
	}

	bridgeRouteLocalnet := filepath.Join("/proc/sys/net/ipv4/conf", bridge, "route_localnet")
	if err := os.WriteFile(bridgeRouteLocalnet, []byte("1"), 0644); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("set %s=1 failed: %w", bridgeRouteLocalnet, err)
	}
	return nil
}

// initBridge will create bridge and add iptable rule.
func initBridge(ipRange string, natBackend string) error {
	if _, err := netlink.LinkByName(bridgeName); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			logrus.Warnf("check bridge %s exists failed: %v", bridgeName, err)
			return err
		}
		logrus.Infof("bridge %s not exists, create it", bridgeName)
		if err = netlink.LinkAdd(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}); err != nil {
			return err
		}
		addr, err := netlink.ParseAddr(ipRange)
		if err != nil {
			return err
		}
		if err = netlink.AddrAdd(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}, addr); err != nil {
			return err
		}
		if err = netlink.LinkSetUp(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}); err != nil {
			return err
		}
	}

	bridgeLink, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}
	macAddress, _ := net.ParseMAC(bridgeMac)
	if err = netlink.LinkSetHardwareAddr(bridgeLink, macAddress); err != nil {
		return err
	}

	if m, ok := networkmanager.NetworkManagers[natBackend]; !ok {
		return fmt.Errorf("no corresponding network manager for natBackend: %s", natBackend)
	} else if err = m.SetupSNATRules(ipRange); err != nil {
		return err
	}
	if err = ensureIPv4Forwarding(bridgeName); err != nil {
		return err
	}

	return nil
}

// cleanBridge is used to clean bridge and iptable rule after init failed.
func cleanBridge(natBackend string) error {
	if bridge, err := netlink.LinkByName(bridgeName); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return nil
		}
	} else if err = netlink.LinkDel(bridge); err != nil {
		return err
	}

	if m, ok := networkmanager.NetworkManagers[natBackend]; !ok {
		return fmt.Errorf("no corresponding network manager for natBackend: %s", natBackend)
	} else if err := m.CleanupSNATRules(defaultIpRange); err != nil {
		return err
	}

	return nil
}
