package resources

import (
	"fmt"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	cmap "github.com/orcaman/concurrent-map/v2"
)

func TestCgroupAllocateRetryRequiresExactBindingContract(t *testing.T) {
	index := cmap.New[string]()
	index.Set("allocation-1", "/axern/cgroup-1")
	leases := cmap.New[*apipb.CgroupLease]()
	leases.Set("/axern/cgroup-1", &apipb.CgroupLease{
		CgroupID: "/axern/cgroup-1", AllocationID: "allocation-1",
		State:              apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		OwnerKind:          apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD,
		MemoryRequestBytes: 64, MemoryLimitBytes: 128, CapacityReservationBytes: 64,
	})
	manager := &CgroupManager{allocationLeases: &index, leases: leases}

	resource, err := manager.Allocate(AllocateOption{ContainerID: "allocation-1", MemoryRequestBytes: 64, MemoryLimitBytes: 128})
	if err != nil || resource.ToString() != "/axern/cgroup-1" {
		t.Fatalf("exact retry = %v, %v", resource, err)
	}
	if _, err := manager.Allocate(AllocateOption{ContainerID: "allocation-1", MemoryRequestBytes: 65, MemoryLimitBytes: 128}); err == nil {
		t.Fatal("mismatched retry unexpectedly reused the cgroup binding")
	}
}

func TestAllocationResourceIndexesRemainExactAtNodeScale(t *testing.T) {
	const allocations = 10_000
	cgroupIndex := cmap.New[string]()
	networkIndex := cmap.New[string]()
	for i := 0; i < allocations; i++ {
		id := fmt.Sprintf("allocation-%05d", i)
		cgroupIndex.Set(id, fmt.Sprintf("/axern/%05d", i))
		networkIndex.Set(id, fmt.Sprintf("network-%05d", i))
	}
	cgroups := &CgroupManager{allocationLeases: &cgroupIndex}
	networks := &InterfaceManager{allocationLeases: &networkIndex}
	for i := 0; i < allocations; i++ {
		id := fmt.Sprintf("allocation-%05d", i)
		if got, ok := cgroups.AllocationResource(id); !ok || got != fmt.Sprintf("/axern/%05d", i) {
			t.Fatalf("cgroup binding %s = %q, %v", id, got, ok)
		}
		if got, ok := networks.AllocationResource(id); !ok || got != fmt.Sprintf("network-%05d", i) {
			t.Fatalf("network binding %s = %q, %v", id, got, ok)
		}
	}
	if _, ok := cgroups.AllocationResource("allocation-missing"); ok {
		t.Fatal("missing cgroup allocation unexpectedly resolved")
	}
	if _, ok := networks.AllocationResource("allocation-missing"); ok {
		t.Fatal("missing network allocation unexpectedly resolved")
	}
}

func BenchmarkAllocationResourceIndex(b *testing.B) {
	index := cmap.New[string]()
	for i := 0; i < 10_000; i++ {
		index.Set(fmt.Sprintf("allocation-%05d", i), fmt.Sprintf("resource-%05d", i))
	}
	manager := &CgroupManager{allocationLeases: &index}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.AllocationResource(fmt.Sprintf("allocation-%05d", i%10_000))
	}
}
