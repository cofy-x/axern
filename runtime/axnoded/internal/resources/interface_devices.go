package resources

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func (m *InterfaceManager) updateInterfacesCache() {
	start := time.Now()
	defer func() {
		logrus.Debugf("load all interfaces cost: %v ms", time.Since(start).Milliseconds())
	}()

	m.allInterfaces, _ = net.Interfaces()
}

// createDevice is used to add a veth pair and move the peer side into a
// persistent named netns so standard runsc can join it through OCI spec.
func (m *InterfaceManager) createDevice(ip string) error {
	hostVethName, peerVethName := ipToVeth(ip)
	nsName := netnsName(ip)

	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil && !isLinkAbsent(err) {
		return fmt.Errorf("get host device %s failed: %w", hostVethName, err)
	}
	if hostVeth != nil {
		if err = netlink.LinkDel(hostVeth); err != nil {
			return fmt.Errorf("delete stale host device %s failed: %v", hostVethName, err)
		}
	}
	_ = runIP("netns", "delete", nsName)

	hostVeth = &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: hostVethName},
		PeerName:  peerVethName,
	}
	if err = netlink.LinkAdd(hostVeth); err != nil {
		return fmt.Errorf("create ip link failed: %v", err)
	}
	if err = runIP("netns", "add", nsName); err != nil {
		_ = netlink.LinkDel(hostVeth)
		return err
	}
	if err = netlink.LinkSetUp(hostVeth); err != nil {
		return fmt.Errorf("set host veth up failed: %v", err)
	}
	if hostVeth.Attrs().MasterIndex == 0 {
		if err = netlink.LinkSetMaster(hostVeth, m.bridgeLink); err != nil {
			return fmt.Errorf("LinkSetMaster failed: %v", err)
		}
	}
	if err = runIP("link", "set", peerVethName, "netns", nsName); err != nil {
		return err
	}
	if err = runIP("netns", "exec", nsName, "ip", "link", "set", "lo", "up"); err != nil {
		return err
	}
	if err = runIP("netns", "exec", nsName, "ip", "link", "set", peerVethName, "name", containerEthName); err != nil {
		return err
	}
	if err = runIP("netns", "exec", nsName, "ip", "addr", "add",
		fmt.Sprintf("%s/%d", ip, prefixLength(m.mask)),
		"dev", containerEthName); err != nil {
		return err
	}
	if err = runIP("netns", "exec", nsName, "ip", "link", "set", containerEthName, "up"); err != nil {
		return err
	}
	if err = runIP("netns", "exec", nsName, "ip", "route", "replace", "default", "via", m.BridgeIp.String()); err != nil {
		return err
	}

	return nil
}

func (m *InterfaceManager) validateDevice(dev *NetResource) error {
	if dev == nil || dev.Ip == nil || dev.Ip.To4() == nil {
		return fmt.Errorf("network resource has no valid IPv4 address")
	}
	ip := dev.Ip.String()
	hostVethName, _ := ipToVeth(ip)
	link, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return fmt.Errorf("host veth %s is not available: %w", hostVethName, err)
	}
	if link.Attrs() == nil || link.Attrs().Index <= 0 {
		return fmt.Errorf("host veth %s has no valid link index", hostVethName)
	}
	return nil
}

func (m *InterfaceManager) validateDeviceConfiguration(dev *NetResource) error {
	if err := m.validateDevice(dev); err != nil {
		return err
	}
	ip := dev.Ip.String()

	nsName := netnsName(ip)
	if _, err := runIPOutput("netns", "exec", nsName, "ip", "-o", "link", "show", containerEthName); err != nil {
		return fmt.Errorf("container interface %s is not available in %s: %w", containerEthName, nsName, err)
	}
	addrOutput, err := runIPOutput("netns", "exec", nsName, "ip", "-o", "-4", "addr", "show", "dev", containerEthName)
	if err != nil {
		return fmt.Errorf("container interface %s has no IPv4 address in %s: %w", containerEthName, nsName, err)
	}
	if !strings.Contains(addrOutput, ip+"/") {
		return fmt.Errorf("container interface %s in %s has unexpected IPv4 address: %s", containerEthName, nsName, strings.TrimSpace(addrOutput))
	}
	routeOutput, err := runIPOutput("netns", "exec", nsName, "ip", "route", "show", "default")
	if err != nil {
		return fmt.Errorf("container interface %s has no default route in %s: %w", containerEthName, nsName, err)
	}
	if !strings.Contains(routeOutput, m.BridgeIp.String()) {
		return fmt.Errorf("container interface %s in %s has unexpected default route: %s", containerEthName, nsName, strings.TrimSpace(routeOutput))
	}
	return nil
}

func (m *InterfaceManager) rebuildDevice(dev *NetResource) (*NetResource, error) {
	if dev == nil || dev.Ip == nil || dev.Ip.To4() == nil {
		return nil, fmt.Errorf("network resource has no valid IPv4 address")
	}
	ip := dev.Ip.String()
	hostVethName, _ := ipToVeth(ip)
	_ = m.destroyDevice(net.Interface{Name: hostVethName})
	if err := m.createInterfaceDevice(ip); err != nil {
		return nil, err
	}
	intf, err := m.lookupInterface(hostVethName)
	if err != nil {
		return nil, fmt.Errorf("lookup interface %s failed: %w", hostVethName, err)
	}
	return &NetResource{
		Interface: intf,
		Ip:        net.ParseIP(ip),
		Mask:      m.mask,
		Gateway:   m.BridgeIp,
		Type:      "bridge",
		NetNSPath: netnsPath(ip),
	}, nil
}

func (m *InterfaceManager) destroyDevice(dev net.Interface) error {
	ip := vethToIP(dev.Name)
	hostVethName, _ := ipToVeth(ip.String())
	nsName := netnsName(ip.String())

	_ = runIP("netns", "delete", nsName)
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil && !isLinkAbsent(err) {
		return fmt.Errorf("get host device %s failed: %w", hostVethName, err)
	}
	if hostVeth != nil {
		if err = netlink.LinkDel(hostVeth); err != nil {
			return err
		}
	}

	return nil
}

func isLinkAbsent(err error) bool {
	if err == nil {
		return false
	}
	var notFound netlink.LinkNotFoundError
	return errors.As(err, &notFound) ||
		errors.Is(err, unix.ENODEV) ||
		errors.Is(err, unix.ENOENT) ||
		errors.Is(err, unix.ESRCH)
}

func runIPOutput(args ...string) (string, error) {
	cmd := exec.Command("ip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip %s failed: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}
