package resources

import (
	"errors"
	"net"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type networkLedgerStore struct {
	ledger *apipb.NetworkLedger
	fail   bool
}

func (s *networkLedgerStore) SaveSnapshot(_ string, value proto.Message) error {
	if s.fail {
		return errors.New("injected ledger failure")
	}
	s.ledger = proto.Clone(value).(*apipb.NetworkLedger)
	return nil
}

func (s *networkLedgerStore) LoadSnapshot(_ string, value proto.Message) error {
	if s.ledger == nil {
		return errord.ErrNotFound
	}
	proto.Merge(value, s.ledger)
	return nil
}

func newNetworkLedgerTestManager(store stateStore) *InterfaceManager {
	return &InterfaceManager{
		interfaces:         queue.New(""),
		idleIp:             queue.New(""),
		usingInterfaces:    cmap.New[struct{}](),
		db:                 store,
		deleteNeighborFunc: func(net.IP) error { return nil },
	}
}

func TestNetworkLeaseIsDurableBeforeAllocationReturns(t *testing.T) {
	store := &networkLedgerStore{}
	manager := newNetworkLedgerTestManager(store)
	resource := &NetResource{Interface: &net.Interface{Name: "hv.ac110002"}, Ip: net.ParseIP("172.17.0.2"), NetNSPath: "/var/run/netns/172.17.0.2"}

	require.NoError(t, manager.markInterfaceUsing("allocation-1", resource))
	require.Equal(t, []*apipb.NetworkLease{{
		AllocationID: "allocation-1", HostInterfaceName: "hv.ac110002",
		Ip: "172.17.0.2", NetnsPath: "/var/run/netns/172.17.0.2",
	}}, store.ledger.GetLeases())
	require.Equal(t, resource.ToString(), mustAllocationResource(t, manager, "allocation-1"))
}

func TestNetworkLeasePersistenceFailureRollsBackAllocation(t *testing.T) {
	store := &networkLedgerStore{fail: true}
	manager := newNetworkLedgerTestManager(store)
	resource := &NetResource{Interface: &net.Interface{Name: "hv.ac110002"}, Ip: net.ParseIP("172.17.0.2"), NetNSPath: "/var/run/netns/172.17.0.2"}

	require.ErrorContains(t, manager.markInterfaceUsing("allocation-1", resource), "injected ledger failure")
	_, found := manager.AllocationResource("allocation-1")
	require.False(t, found)
	require.False(t, manager.usingInterfaces.Has(resource.ToString()))
}

func TestNetworkLedgerRejectsDuplicateAllocationOwnership(t *testing.T) {
	store := &networkLedgerStore{ledger: &apipb.NetworkLedger{Leases: []*apipb.NetworkLease{
		{AllocationID: "allocation-1", HostInterfaceName: "host-1", Ip: "172.17.0.2", NetnsPath: "/var/run/netns/one"},
		{AllocationID: "allocation-1", HostInterfaceName: "host-2", Ip: "172.17.0.3", NetnsPath: "/var/run/netns/two"},
	}}}
	_, err := NewInterfaceManager(store, "172.17.0.0/24", 8, 2, "bridge")
	require.ErrorContains(t, err, "duplicate ownership")
}

func TestNetworkRecyclePersistenceFailureKeepsResourceQuarantinedForRetry(t *testing.T) {
	store := &networkLedgerStore{}
	manager := newNetworkLedgerTestManager(store)
	manager.destroyDeviceFunc = func(net.Interface) error { return nil }
	resource := &NetResource{Interface: &net.Interface{Name: "hv.ac110002"}, Ip: net.ParseIP("172.17.0.2"), NetNSPath: "/var/run/netns/172.17.0.2"}
	require.NoError(t, manager.markInterfaceUsing("allocation-1", resource))
	store.fail = true

	require.ErrorContains(t, manager.Recycle(resource.ToString()), "injected ledger failure")
	require.Equal(t, resource.ToString(), mustAllocationResource(t, manager, "allocation-1"))
	require.True(t, manager.usingInterfaces.Has(resource.ToString()))
	require.Zero(t, manager.idleIp.Length())
}

func mustAllocationResource(t *testing.T, manager *InterfaceManager, allocationID string) string {
	t.Helper()
	value, ok := manager.AllocationResource(allocationID)
	require.True(t, ok)
	return value
}
