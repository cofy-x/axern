package resources

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"google.golang.org/protobuf/proto"
)

func recoveredNetworkFixture(t *testing.T) (*InterfaceManager, *NetResource, map[string]netlink.Link, *netlink.Veth) {
	t.Helper()
	manager := newNetworkLedgerTestManager(&networkLedgerStore{})
	manager.IpRange = "172.17.0.1/24"
	manager.BridgeIp = net.ParseIP("172.17.0.1")
	resource := &NetResource{
		Interface: &net.Interface{Name: "hv.ac110002"},
		Ip:        net.ParseIP("172.17.0.2"),
		NetNSPath: netnsPath("172.17.0.2"),
	}
	require.NoError(t, manager.markInterfaceUsing("allocation-1", resource))
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName, Index: 9}}
	manager.bridgeLink = bridge
	host := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{
		Name: resource.Interface.Name, Index: 10, ParentIndex: 20, MasterIndex: 9, NetNsID: 7, Flags: net.FlagUp,
	}}
	peer := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{
		Name: containerEthName, Index: 20, ParentIndex: 10, Flags: net.FlagUp,
	}}
	links := map[string]netlink.Link{bridgeName: bridge, resource.Interface.Name: host}
	manager.recoveryLinkLookupFunc = func(name string) (netlink.Link, error) {
		link, ok := links[name]
		if !ok {
			return nil, errors.New("link absent")
		}
		return link, nil
	}
	manager.recoveryNamespaceFunc = func(path string, pid int) (netlink.Link, int, error) {
		require.Equal(t, resource.NetNSPath, path)
		require.Equal(t, 123, pid)
		return peer, 7, nil
	}
	return manager, resource, links, peer
}

func TestNetworkRecoveryPreservesAssignedLeaseWithoutPreparingInterface(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "present"}[present], func(t *testing.T) {
			manager, resource, _, _ := recoveredNetworkFixture(t)
			manager.validateDeviceConfigurationFunc = func(*NetResource) error {
				t.Fatal("assigned interfaces must not be checked as idle interfaces")
				return nil
			}
			manager.destroyDeviceFunc = func(net.Interface) error {
				t.Fatal("assigned interfaces must not be rebuilt")
				return nil
			}
			var interfaces []net.Interface
			if present {
				interfaces = []net.Interface{{Name: resource.Interface.Name, Index: 42}}
			}
			ips := map[string]struct{}{resource.Ip.String(): {}}
			require.NoError(t, manager.loadInterfaces(ips, interfaces))
			require.Equal(t, resource.ToString(), mustAllocationResource(t, manager, "allocation-1"))
			require.True(t, manager.usingInterfaces.Has(resource.ToString()))
			require.Empty(t, ips)
			require.Zero(t, manager.interfaces.Length())
			require.Zero(t, manager.idleIp.Length())
		})
	}
}

func TestNetworkRecoveryUsesTopologyAfterRunscConsumesL3Configuration(t *testing.T) {
	manager, resource, _, _ := recoveredNetworkFixture(t)
	manager.validateDeviceConfigurationFunc = func(*NetResource) error {
		t.Fatal("running netstack must not require Linux interface addresses or routes")
		return nil
	}
	store := manager.db.(*networkLedgerStore)
	before := proto.Clone(store.ledger)
	require.NoError(t, manager.VerifyRecoveredNetworkBindings(map[string]int{"allocation-1": 123}))
	require.True(t, proto.Equal(before, store.ledger), "recovery must not rewrite the ledger")
	require.Equal(t, resource.ToString(), mustAllocationResource(t, manager, "allocation-1"))
}

func TestNetworkRecoveryRejectsIdenticalBindingsOwnedByDifferentAllocations(t *testing.T) {
	manager, resource, _, _ := recoveredNetworkFixture(t)
	manager.allocationLeases.Set("allocation-2", resource.ToString())
	require.ErrorContains(t, manager.loadInterfaces(map[string]struct{}{}, nil), "assigns interface hv.ac110002 more than once")
}

func TestNetworkRecoveryDoesNotRebuildUnassignedInterfaceWithConsumedAddresses(t *testing.T) {
	manager, _, _, _ := recoveredNetworkFixture(t)
	manager.allocationLeases.Clear()
	manager.usingInterfaces.Clear()
	manager.validateDeviceConfigurationFunc = func(*NetResource) error {
		return errors.New("container interface eth0 has no address")
	}
	manager.destroyDeviceFunc = func(net.Interface) error {
		t.Fatal("missing ownership must not authorize destroying an interface")
		return nil
	}
	manager.createDeviceFunc = func(string) error {
		t.Fatal("missing ownership must not authorize replacing an interface")
		return nil
	}
	manager.deleteNeighborFunc = func(net.IP) error {
		t.Fatal("unverified interface must not change neighbor state")
		return nil
	}
	ips := map[string]struct{}{"172.17.0.2": {}}
	err := manager.loadInterfaces(ips, []net.Interface{{Name: "hv.ac110002", Index: 42}})
	require.ErrorContains(t, err, "validate unassigned interface hv.ac110002 before pool recovery")
	require.Zero(t, manager.interfaces.Length())
	require.Zero(t, manager.idleIp.Length())
	require.Contains(t, ips, "172.17.0.2")
}

func TestNetworkRecoveryRejectsConflictingKernelBindingsWithoutReleasingLease(t *testing.T) {
	tests := []struct {
		name   string
		change func(*InterfaceManager, *NetResource, map[string]netlink.Link, *netlink.Veth)
		want   string
	}{
		{"missing bridge", func(_ *InterfaceManager, _ *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			delete(links, bridgeName)
		}, "read network bridge"},
		{"replaced bridge", func(_ *InterfaceManager, _ *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[bridgeName] = &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Index: 99}}
		}, "bridge identity changed"},
		{"wrong bridge kind", func(_ *InterfaceManager, _ *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[bridgeName] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: 9}}
		}, "bridge identity changed"},
		{"missing host veth", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			delete(links, r.Interface.Name)
		}, "read host veth"},
		{"wrong host kind", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name] = &netlink.Dummy{LinkAttrs: *links[r.Interface.Name].Attrs()}
		}, "not the assigned veth"},
		{"wrong host name", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().Name = "other"
		}, "not the assigned veth"},
		{"wrong host bridge", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().MasterIndex = 99
		}, "not up on the assigned bridge"},
		{"host link down", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().Flags = 0
		}, "not up on the assigned bridge"},
		{"wrong peer namespace", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().NetNsID = 8
		}, "not the assigned peer pair"},
		{"missing peer namespace identity", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().NetNsID = -1
		}, "not the assigned peer pair"},
		{"wrong host peer index", func(_ *InterfaceManager, r *NetResource, links map[string]netlink.Link, _ *netlink.Veth) {
			links[r.Interface.Name].Attrs().ParentIndex = 21
		}, "not the assigned peer pair"},
		{"wrong namespace peer index", func(_ *InterfaceManager, _ *NetResource, _ map[string]netlink.Link, peer *netlink.Veth) {
			peer.ParentIndex = 11
		}, "not the assigned peer pair"},
		{"wrong peer name", func(_ *InterfaceManager, _ *NetResource, _ map[string]netlink.Link, peer *netlink.Veth) {
			peer.Name = "eth1"
		}, "not an active veth"},
		{"peer link down", func(_ *InterfaceManager, _ *NetResource, _ map[string]netlink.Link, peer *netlink.Veth) {
			peer.Flags = 0
		}, "not an active veth"},
		{"unreadable namespace", func(m *InterfaceManager, _ *NetResource, _ map[string]netlink.Link, _ *netlink.Veth) {
			m.recoveryNamespaceFunc = func(string, int) (netlink.Link, int, error) { return nil, 0, errors.New("namespace unreadable") }
		}, "namespace unreadable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, resource, links, peer := recoveredNetworkFixture(t)
			before := resource.ToString()
			test.change(manager, resource, links, peer)
			require.ErrorContains(t, manager.VerifyRecoveredNetworkBindings(map[string]int{"allocation-1": 123}), test.want)
			require.Equal(t, before, mustAllocationResource(t, manager, "allocation-1"))
			require.True(t, manager.usingInterfaces.Has(before))
			require.Zero(t, manager.idleIp.Length())
		})
	}
}

func TestNetworkRecoveryRejectsMissingRuntimePID(t *testing.T) {
	manager, _, _, _ := recoveredNetworkFixture(t)
	for _, pid := range []int{0, -1} {
		require.ErrorContains(t, manager.VerifyRecoveredNetworkBindings(map[string]int{"allocation-1": pid}), "no valid runtime PID")
	}
}

func TestNetworkRecoveryNeverLooksUpAnotherAllocationsLease(t *testing.T) {
	manager, resource, _, _ := recoveredNetworkFixture(t)
	manager.recoveryLinkLookupFunc = func(string) (netlink.Link, error) {
		t.Fatal("a runtime identity cannot acquire another Allocation's network lease")
		return nil, nil
	}
	require.NoError(t, manager.VerifyRecoveredNetworkBindings(map[string]int{"allocation-2": 123}))
	require.Equal(t, resource.ToString(), mustAllocationResource(t, manager, "allocation-1"))
}

func TestNetworkRecoveryChecksNamespaceFileIdentity(t *testing.T) {
	directory := t.TempDir()
	assigned := filepath.Join(directory, "assigned")
	runtime := filepath.Join(directory, "runtime")
	require.NoError(t, os.WriteFile(assigned, nil, 0600))
	require.NoError(t, os.Link(assigned, runtime))
	file, err := openRecoveredNetworkNamespace(assigned, runtime)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, os.Remove(runtime))
	require.NoError(t, os.WriteFile(runtime, nil, 0600))
	_, err = openRecoveredNetworkNamespace(assigned, runtime)
	require.ErrorContains(t, err, "does not match the assigned namespace")
	_, err = openRecoveredNetworkNamespace(assigned, filepath.Join(directory, "missing-runtime"))
	require.ErrorContains(t, err, "stat runtime network namespace")
	_, err = openRecoveredNetworkNamespace(filepath.Join(directory, "missing-assigned"), runtime)
	require.ErrorContains(t, err, "open assigned network namespace")
	_, err = openRecoveredNetworkNamespace("/proc/self/ns/net", "/proc/self/ns/net")
	require.ErrorContains(t, err, "is the host namespace")
}

func TestNetworkRecoveryRejectsNoncanonicalLeaseBindings(t *testing.T) {
	tests := []struct {
		name   string
		change func(*NetResource)
		want   string
	}{
		{"wrong interface", func(r *NetResource) { r.Interface.Name = "hv.ac110003" }, "conflicts with IP"},
		{"wrong namespace", func(r *NetResource) { r.NetNSPath = "/run/netns/another-allocation" }, "conflicts with IP"},
		{"outside range", func(r *NetResource) { r.Ip = net.ParseIP("10.0.0.2") }, "outside the sandbox address range"},
		{"gateway address", func(r *NetResource) { r.Ip = net.ParseIP("172.17.0.1") }, "outside the sandbox address range"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, resource, _, _ := recoveredNetworkFixture(t)
			test.change(resource)
			manager.allocationLeases.Set("allocation-1", resource.ToString())
			require.ErrorContains(t, manager.loadInterfaces(map[string]struct{}{}, nil), test.want)
			require.ErrorContains(t, manager.VerifyRecoveredNetworkBindings(map[string]int{"allocation-1": 123}), test.want)
		})
	}
}

func TestNetworkRecoveryAcceptsCanonicalIPv6Lease(t *testing.T) {
	resource := &NetResource{
		Interface: &net.Interface{Name: "hv.000000000002"},
		Ip:        net.ParseIP("fd31::2"),
		NetNSPath: netnsPath("fd31::2"),
	}
	require.NoError(t, validateNetworkLeaseBinding(resource, "fd31::1/64", net.ParseIP("fd31::1")))
}
