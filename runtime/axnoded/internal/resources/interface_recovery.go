package resources

import (
	"fmt"
	"net"
	"os"
	"sort"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// VerifyRecoveredNetworkBindings audits owned network leases only after the
// caller has classified a complete runtime inventory against durable Allocation
// admission. The values are Sentry host PIDs for running or paused Allocations.
// A missing lease is not proof of network isolation: this ledger does not own
// the Allocation's requested network mode.
func (m *InterfaceManager) VerifyRecoveredNetworkBindings(live map[string]int) error {
	m.leaseMu.Lock()
	defer m.leaseMu.Unlock()
	m.ensureLeaseIndexLocked()
	ids := make([]string, 0, len(live))
	for id := range live {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		binding, owned := m.allocationLeases.Get(id)
		if !owned {
			continue
		}
		if id == "" || live[id] <= 0 {
			return fmt.Errorf("allocation %s has no valid runtime PID for network recovery", id)
		}
		resource, err := NewNetResource(binding)
		if err != nil {
			return fmt.Errorf("decode allocation %s network lease: %w", id, err)
		}
		if err := validateNetworkLeaseBinding(resource, m.IpRange, m.BridgeIp); err != nil {
			return fmt.Errorf("validate allocation %s network lease: %w", id, err)
		}
		if err := m.verifyRecoveredNetworkBinding(resource, live[id]); err != nil {
			return fmt.Errorf("verify allocation %s network binding: %w", id, err)
		}
	}
	return nil
}

func validateNetworkLeaseBinding(resource *NetResource, ipRange string, gateway net.IP) error {
	if resource == nil || resource.Interface == nil || resource.Ip == nil {
		return fmt.Errorf("network lease is incomplete")
	}
	_, addresses, err := net.ParseCIDR(ipRange)
	if err != nil {
		return fmt.Errorf("parse network lease address range: %w", err)
	}
	if !addresses.Contains(resource.Ip) || resource.Ip.Equal(gateway) {
		return fmt.Errorf("network lease IP %s is outside the sandbox address range", resource.Ip)
	}
	host, _ := ipToVeth(resource.Ip.String())
	if resource.Interface.Name != host || !vethToIP(host, ipRange).Equal(resource.Ip) {
		return fmt.Errorf("network lease host interface %s conflicts with IP %s", resource.Interface.Name, resource.Ip)
	}
	if resource.NetNSPath != netnsPath(resource.Ip.String()) {
		return fmt.Errorf("network lease namespace %s conflicts with IP %s", resource.NetNSPath, resource.Ip)
	}
	return nil
}

func (m *InterfaceManager) verifyRecoveredNetworkBinding(resource *NetResource, pid int) error {
	lookup := netlink.LinkByName
	if m.recoveryLinkLookupFunc != nil {
		lookup = m.recoveryLinkLookupFunc
	}
	bridge, err := lookup(bridgeName)
	if err != nil {
		return fmt.Errorf("read network bridge: %w", err)
	}
	if bridge == nil || bridge.Type() != "bridge" || bridge.Attrs() == nil || bridge.Attrs().Index <= 0 || m.bridgeLink == nil || m.bridgeLink.Attrs() == nil || bridge.Attrs().Index != m.bridgeLink.Attrs().Index {
		return fmt.Errorf("network bridge identity changed")
	}
	host, err := lookup(resource.Interface.Name)
	if err != nil {
		return fmt.Errorf("read host veth %s: %w", resource.Interface.Name, err)
	}
	if host == nil || host.Type() != "veth" || host.Attrs() == nil || host.Attrs().Index <= 0 || host.Attrs().Name != resource.Interface.Name {
		return fmt.Errorf("host interface %s is not the assigned veth", resource.Interface.Name)
	}
	if host.Attrs().MasterIndex != bridge.Attrs().Index || host.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("host veth %s is not up on the assigned bridge", resource.Interface.Name)
	}
	inspect := inspectRecoveredNetworkNamespace
	if m.recoveryNamespaceFunc != nil {
		inspect = m.recoveryNamespaceFunc
	}
	peer, peerNamespaceID, err := inspect(resource.NetNSPath, pid)
	if err != nil {
		return err
	}
	if peer == nil || peer.Type() != "veth" || peer.Attrs() == nil || peer.Attrs().Name != containerEthName || peer.Attrs().Index <= 0 || peer.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf("namespace interface %s is not an active veth", containerEthName)
	}
	// Interface indices are only unique within a namespace. Check the host's
	// peer namespace ID as well as both ends' indices so a same-numbered link
	// in an unrelated namespace cannot satisfy the binding.
	if peerNamespaceID < 0 || host.Attrs().NetNsID != peerNamespaceID || host.Attrs().ParentIndex != peer.Attrs().Index || peer.Attrs().ParentIndex != host.Attrs().Index {
		return fmt.Errorf("host veth %s and namespace interface %s are not the assigned peer pair", resource.Interface.Name, containerEthName)
	}
	return nil
}

func inspectRecoveredNetworkNamespace(namespacePath string, pid int) (netlink.Link, int, error) {
	namespace, err := openRecoveredNetworkNamespace(namespacePath, fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, 0, err
	}
	defer namespace.Close()
	handle, err := netlink.NewHandleAt(netns.NsHandle(namespace.Fd()), unix.NETLINK_ROUTE)
	if err != nil {
		return nil, 0, fmt.Errorf("open assigned network namespace %s: %w", namespacePath, err)
	}
	defer handle.Close()
	peer, err := handle.LinkByName(containerEthName)
	if err != nil {
		return nil, 0, fmt.Errorf("read namespace interface %s: %w", containerEthName, err)
	}
	namespaceID, err := netlink.GetNetNsIdByFd(int(namespace.Fd()))
	if err != nil {
		return nil, 0, fmt.Errorf("read peer network namespace identity: %w", err)
	}
	return peer, namespaceID, nil
}

// Keep the namespace open across the netlink audit so replacing the named
// namespace cannot redirect the inspection after the runtime identity check.
func openRecoveredNetworkNamespace(namespacePath, runtimeNamespacePath string) (*os.File, error) {
	namespace, err := os.Open(namespacePath)
	if err != nil {
		return nil, fmt.Errorf("open assigned network namespace %s: %w", namespacePath, err)
	}
	namespaceInfo, err := namespace.Stat()
	if err != nil {
		namespace.Close()
		return nil, fmt.Errorf("stat assigned network namespace: %w", err)
	}
	runtimeInfo, err := os.Stat(runtimeNamespacePath)
	if err != nil {
		namespace.Close()
		return nil, fmt.Errorf("stat runtime network namespace: %w", err)
	}
	if !os.SameFile(namespaceInfo, runtimeInfo) {
		namespace.Close()
		return nil, fmt.Errorf("runtime network namespace does not match the assigned namespace")
	}
	hostInfo, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		namespace.Close()
		return nil, fmt.Errorf("stat host network namespace: %w", err)
	}
	if os.SameFile(namespaceInfo, hostInfo) {
		namespace.Close()
		return nil, fmt.Errorf("assigned network namespace is the host namespace")
	}
	return namespace, nil
}
