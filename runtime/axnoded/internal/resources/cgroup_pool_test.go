//go:build linux

package resources

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	cg "github.com/containerd/cgroups/v3/cgroup1"
	"github.com/containerd/cgroups/v3/cgroup1/stats"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
)

type stubCgroupDriver struct {
	createCalls int
	failFirst   int
}

func (d *stubCgroupDriver) Mode() string            { return os2.CgroupModeV2 }
func (d *stubCgroupDriver) EnsureRoot(string) error { return nil }
func (d *stubCgroupDriver) ResolveRoot(rootName string) (string, error) {
	return "/" + strings.Trim(rootName, "/"), nil
}

func (d *stubCgroupDriver) Create(group string, resources *spec.LinuxResources) (os2.Cgroup, error) {
	d.createCalls++
	if d.failFirst > 0 {
		d.failFirst--
		return nil, fmt.Errorf("create failed")
	}
	return &stubCgroup{}, nil
}

func (d *stubCgroupDriver) Load(group string) (os2.Cgroup, error) { return &stubCgroup{}, nil }

func (d *stubCgroupDriver) ExistingGroups(rootName string) ([]string, error) { return nil, nil }

func (d *stubCgroupDriver) Remove(group string) error { return nil }

func (d *stubCgroupDriver) LocalCPUCount() (int, error) { return 1, nil }

type stubCgroup struct{}

func (c *stubCgroup) Update(resources *spec.LinuxResources) error { return nil }

func (c *stubCgroup) Delete() error { return nil }

func (c *stubCgroup) Stats() (*os2.CgroupStats, error) { return &os2.CgroupStats{}, nil }

func (c *stubCgroup) AddProc(pid uint64) error { return nil }

func (c *stubCgroup) Processes(recursive bool) ([]int, error) { return nil, nil }

type stubCgroupRetirementMemory struct {
	domain      *hostlinux.CgroupMemoryDomain
	observation *hostlinux.CgroupMemoryObservation
	inspectErr  error
	readErr     error
}

func (s *stubCgroupRetirementMemory) InspectParent(string) (*hostlinux.CgroupMemoryDomain, error) {
	return s.domain, s.inspectErr
}

func (s *stubCgroupRetirementMemory) ReadObservation(string) (*hostlinux.CgroupMemoryObservation, error) {
	return s.observation, s.readErr
}

func TestCgroupManagerAllocateLazilyCreatesWhenPoolIsEmpty(t *testing.T) {
	driver := &stubCgroupDriver{}
	manager := &CgroupManager{
		size: 2, cacheSize: 0, rootName: "sandbox",
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), generator: truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		storeMark: atomic.Bool{}, gcQueue: queue.New(""), cgroupDriver: driver, db: discardStateStore{},
	}

	resource, err := manager.Allocate(AllocateOption{
		ContainerID: "lazy-create", AllocationAttempt: 7, RuntimeName: "runsc",
		MemoryRequestBytes: 512, MemoryLimitBytes: 1024,
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, resource.ToString())
	assert.Equal(t, 1, driver.createCalls)
	assert.Equal(t, 1, manager.UsingNum())
	lease, _ := manager.leases.Get(resource.ToString())
	assert.Equal(t, int64(7), lease.GetAllocationAttempt())
	assert.Equal(t, int64(1024), lease.GetMemoryLimitBytes())
	assert.Equal(t, "runsc", lease.GetRuntimeName())
}

func TestBoundedCgroupDiagnosticPreservesUTF8(t *testing.T) {
	message := strings.Repeat("界", 400)
	bounded := boundedCgroupDiagnostic(message)
	if len(bounded) > 1024 || !utf8.ValidString(bounded) {
		t.Fatalf("bounded diagnostic length=%d valid=%t", len(bounded), utf8.ValidString(bounded))
	}
}

func TestCgroupManagerRequiredMemoryAdmissionGatesZeroRequest(t *testing.T) {
	driver := &stubCgroupDriver{}
	manager := &CgroupManager{
		size: 2, cacheSize: 0, rootName: "sandbox", memoryAdmissionRequired: true,
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), generator: truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		storeMark: atomic.Bool{}, gcQueue: queue.New(""), cgroupDriver: driver, db: discardStateStore{},
	}

	if _, err := manager.Allocate(AllocateOption{ContainerID: "warming"}); err == nil {
		t.Fatal("Allocate() accepted create before the first node memory budget sample")
	}
	manager.memoryCapacity = MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, SystemReserveExhausted: true,
		CapacityIdentity: "boot:mount", SampledAt: time.Now().UTC(),
	}
	if _, err := manager.Allocate(AllocateOption{ContainerID: "exhausted"}); err == nil {
		t.Fatal("Allocate() accepted create while node system reserve was exhausted")
	}
	manager.memoryCapacity.SystemReserveExhausted = false
	if _, err := manager.Allocate(AllocateOption{ContainerID: "healthy"}); err != nil {
		t.Fatalf("Allocate() rejected healthy zero-request create: %v", err)
	}
}

func TestMemoryCapacityInvalidationClosesLocalAdmissionImmediately(t *testing.T) {
	manager := &CgroupManager{
		memoryAdmissionRequired: true,
		usingID:                 cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
	}
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, CapacityIdentity: "boot-a:mount-a", SampledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{Unavailable: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Allocate(AllocateOption{ContainerID: "must-fail"}); err == nil {
		t.Fatal("Allocate() reused capacity after explicit invalidation")
	}
}

func TestMemoryCapacityIdentityChangeWithCommitmentFailsClosed(t *testing.T) {
	manager := &CgroupManager{
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
	}
	manager.leases.Set("/sandbox/assigned", &apipb.CgroupLease{
		CgroupID: "/sandbox/assigned", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-a", AssignedAtUnixNano: 1, MemoryRequestBytes: 1024,
	})
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, CapacityIdentity: "boot-a:mount-a", SampledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, CapacityIdentity: "boot-b:mount-b", SampledAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("UpdateMemoryCapacity() accepted an identity change with durable commitment")
	}
	if !manager.memoryCapacity.SampledAt.IsZero() {
		t.Fatal("identity mismatch retained the old local admission sample")
	}
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, CapacityIdentity: "boot-b:mount-b", SampledAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("UpdateMemoryCapacity() reopened admission while the old commitment remained")
	}
	manager.leases.Remove("/sandbox/assigned")
	if err := manager.UpdateMemoryCapacity(MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: 1 << 30, CapacityIdentity: "boot-b:mount-b", SampledAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateMemoryCapacity() did not accept the new identity after cleanup: %v", err)
	}
}

func TestCgroupManagerAddReleasesIDAfterCreateFailure(t *testing.T) {
	driver := &stubCgroupDriver{failFirst: 1}
	manager := &CgroupManager{
		size: 2, cacheSize: 0, rootName: "sandbox",
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), generator: truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		storeMark: atomic.Bool{}, gcQueue: queue.New(""), cgroupDriver: driver, db: discardStateStore{},
	}

	manager.Add(1)
	assert.Equal(t, 1, driver.createCalls)
	assert.Equal(t, 0, manager.cgroups.Count())

	resource, err := manager.Allocate(AllocateOption{ContainerID: "retry"})
	assert.NoError(t, err)
	assert.NotEmpty(t, resource.ToString())
	assert.Equal(t, 2, driver.createCalls)
	assert.Equal(t, 1, manager.UsingNum())
}

func TestCgroupManagerRecycleIsOneWayIntoRetirement(t *testing.T) {
	manager := &CgroupManager{
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
		retirementMemory: &stubCgroupRetirementMemory{
			domain: &hostlinux.CgroupMemoryDomain{
				BootID: "boot-a", MountIdentity: "mount-a", ParentInode: 11,
				LimitBytes: 2048, SwapMaxBytes: 0, OOMGroup: true,
			},
			observation: &hostlinux.CgroupMemoryObservation{CurrentBytes: 1536},
		},
	}
	id := "/sandbox/assigned"
	manager.usingID.Set(id, struct{}{})
	manager.cgroups.Set(id, struct{}{})
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-a", AllocationAttempt: 3, RuntimeName: "runc",
		MemoryRequestBytes: 1024, MemoryLimitBytes: 2048, AssignedAtUnixNano: 1,
		CgroupBootID: "boot-a", CgroupMountIdentity: "mount-a", CgroupParentInode: 11, CgroupLeafInode: 12,
	})
	if err := manager.Recycle(id); err != nil {
		t.Fatalf("Recycle() error = %v", err)
	}
	lease, _ := manager.leases.Get(id)
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING || manager.idleID.Has(id) || manager.usingID.Has(id) {
		t.Fatalf("recycled lease = %+v, idle=%t using=%t", lease, manager.idleID.Has(id), manager.usingID.Has(id))
	}
	if lease.GetCurrentChargedBytes() != 1536 {
		t.Fatalf("retiring current charge = %d, want 1536", lease.GetCurrentChargedBytes())
	}
	commitment := manager.MemoryCommitment()
	if commitment.CommittedBytes != 1536 || commitment.CleanupDebtBytes != 1536 {
		t.Fatalf("retiring memory commitment = %+v", commitment)
	}
	if err := manager.Recycle(id); err != nil {
		t.Fatalf("idempotent Recycle() error = %v", err)
	}
	retiring := manager.RetiringMemoryLeases()
	if len(retiring) != 1 || retiring[0].AllocationID != "alloc-a" || retiring[0].AllocationAttempt != 3 ||
		retiring[0].RuntimeName != "runc" || retiring[0].MemoryRequest != 1024 || retiring[0].MemoryLimit != 2048 ||
		retiring[0].BootID != "boot-a" || retiring[0].MountIdentity != "mount-a" ||
		retiring[0].ParentInode != 11 || retiring[0].LeafInode != 12 {
		t.Fatalf("RetiringMemoryLeases() = %+v", retiring)
	}
}

func TestCgroupManagerRecycleFailsClosedBeforeIdentityMismatchCanReleaseCommitment(t *testing.T) {
	manager := &CgroupManager{
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
		retirementMemory: &stubCgroupRetirementMemory{
			domain: &hostlinux.CgroupMemoryDomain{
				BootID: "boot-a", MountIdentity: "mount-b", ParentInode: 11,
				LimitBytes: 2048, SwapMaxBytes: 0, OOMGroup: true,
			},
		},
	}
	id := "/sandbox/assigned"
	manager.usingID.Set(id, struct{}{})
	manager.cgroups.Set(id, struct{}{})
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-a", MemoryRequestBytes: 1024, MemoryLimitBytes: 2048, AssignedAtUnixNano: 1,
		CgroupBootID: "boot-a", CgroupMountIdentity: "mount-a", CgroupParentInode: 11, CgroupLeafInode: 12,
	})

	if err := manager.Recycle(id); err == nil {
		t.Fatal("Recycle() accepted a changed cgroup identity")
	}
	lease, _ := manager.leases.Get(id)
	if lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED || !manager.usingID.Has(id) {
		t.Fatalf("failed retirement released durable ownership: lease=%+v using=%t", lease, manager.usingID.Has(id))
	}
	if commitment := manager.MemoryCommitment(); commitment.CommittedBytes != 1024 || commitment.CleanupDebtBytes != 0 {
		t.Fatalf("failed retirement commitment = %+v", commitment)
	}
}

func TestCgroupManagerRecycleChargesRequestOnlyAllocationCurrentMemory(t *testing.T) {
	manager := &CgroupManager{
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
		retirementMemory: &stubCgroupRetirementMemory{
			domain:      &hostlinux.CgroupMemoryDomain{BootID: "boot-a", MountIdentity: "mount-a", ParentInode: 31, LimitBytes: -1, SwapMaxBytes: -1},
			observation: &hostlinux.CgroupMemoryObservation{CurrentBytes: 4096},
		},
	}
	id := "/sandbox/request-only"
	manager.usingID.Set(id, struct{}{})
	manager.cgroups.Set(id, struct{}{})
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-request-only", AllocationAttempt: 5, RuntimeName: "runsc",
		MemoryRequestBytes: 1024, AssignedAtUnixNano: 1,
	})

	if err := manager.Recycle(id); err != nil {
		t.Fatalf("Recycle() error = %v", err)
	}
	lease, _ := manager.leases.Get(id)
	if lease.GetCurrentChargedBytes() != 4096 {
		t.Fatalf("retiring current charge = %d, want 4096", lease.GetCurrentChargedBytes())
	}
	if commitment := manager.MemoryCommitment(); commitment.CommittedBytes != 4096 || commitment.CleanupDebtBytes != 4096 {
		t.Fatalf("request-only retiring commitment = %+v", commitment)
	}
	retiring := manager.RetiringMemoryLeases()
	if len(retiring) != 1 || retiring[0].MemoryRequest != 1024 || retiring[0].MemoryLimit != 0 {
		t.Fatalf("request-only RetiringMemoryLeases() = %+v", retiring)
	}
}

func TestBindMemoryDomainIsDurableAndIdentityImmutable(t *testing.T) {
	manager := &CgroupManager{
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), db: discardStateStore{},
	}
	id := "/sandbox/assigned"
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-a", MemoryRequestBytes: 1024, MemoryLimitBytes: 2048, AssignedAtUnixNano: 1,
	})
	if err := manager.BindMemoryDomain(id, "alloc-a", 2048, "boot-a", "mount-a", 11, 12); err != nil {
		t.Fatalf("BindMemoryDomain() error = %v", err)
	}
	if err := manager.BindMemoryDomain(id, "alloc-a", 2048, "boot-a", "mount-a", 11, 12); err != nil {
		t.Fatalf("idempotent BindMemoryDomain() error = %v", err)
	}
	if err := manager.BindMemoryDomain(id, "alloc-a", 2048, "boot-a", "mount-b", 11, 12); err == nil {
		t.Fatal("BindMemoryDomain() replaced a durable kernel identity")
	}
	lease, _ := manager.leases.Get(id)
	if lease.GetCgroupMountIdentity() != "mount-a" || lease.GetCgroupParentInode() != 11 || lease.GetCgroupLeafInode() != 12 {
		t.Fatalf("bound lease = %+v", lease)
	}
}

func TestValidateCgroupLeaseRejectsPartialMemoryIdentity(t *testing.T) {
	err := validateCgroupLease(&apipb.CgroupLease{
		CgroupID: "/sandbox/assigned", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "alloc-a", MemoryLimitBytes: 2048, AssignedAtUnixNano: 1, CgroupBootID: "boot-a",
	})
	if err == nil {
		t.Fatal("validateCgroupLease() accepted a partial memory identity")
	}
}

func TestValidateCgroupLeaseRequiresAssignedRuntimeForNodeLocalAllocation(t *testing.T) {
	err := validateCgroupLease(&apipb.CgroupLease{
		CgroupID: "/sandbox/assigned", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "node-local", AssignedAtUnixNano: 1,
	})
	if err == nil {
		t.Fatal("validateCgroupLease() accepted assigned ownership without a runtime")
	}
}

func TestValidateCgroupLeaseRejectsInvalidUTF8IdentityDiagnostic(t *testing.T) {
	err := validateCgroupLease(&apipb.CgroupLease{
		CgroupID: "/sandbox/retiring", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		RetiringAtUnixNano: 1, LastIdentityVerificationError: string([]byte{0xff}),
	})
	if err == nil {
		t.Fatal("validateCgroupLease() accepted invalid UTF-8 identity diagnostic")
	}
}

type MockCgroup struct {
	path string
}

func (m *MockCgroup) New(s string, resources *spec.LinuxResources) (cg.Cgroup, error) {
	return &MockCgroup{
		path: s,
	}, nil
}

func (m *MockCgroup) Add(process cg.Process, name ...cg.Name) error {
	return nil
}

func (m *MockCgroup) AddProc(u uint64, name ...cg.Name) error {
	return nil

}

func (m *MockCgroup) AddTask(process cg.Process, name ...cg.Name) error {
	return nil

}

func (m *MockCgroup) Delete() error {
	return nil

}

func (m *MockCgroup) MoveTo(cgroup cg.Cgroup) error {
	return nil

}

func (m *MockCgroup) Stat(handler ...cg.ErrorHandler) (*stats.Metrics, error) {
	return nil, nil

}

func (m *MockCgroup) Update(resources *spec.LinuxResources) error {
	return nil

}

func (m *MockCgroup) Processes(name cg.Name, b bool) ([]cg.Process, error) {
	return nil, nil
}

func (m *MockCgroup) Tasks(name cg.Name, b bool) ([]cg.Task, error) {
	return nil, nil
}

func (m *MockCgroup) Freeze() error {
	return nil

}

func (m *MockCgroup) Thaw() error {
	return nil

}

func (m *MockCgroup) OOMEventFD() (uintptr, error) {
	return 0, nil
}

func (m *MockCgroup) RegisterMemoryEvent(event cg.MemoryEvent) (uintptr, error) {
	return 0, nil
}

func (m *MockCgroup) State() cg.State {
	return cg.Unknown
}

func (m *MockCgroup) Subsystems() []cg.Subsystem {
	return []cg.Subsystem{}
}

var _ cg.Cgroup = &MockCgroup{}
