package resources

import (
	"context"
	"errors"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/queue"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

type discardStateStore struct{}

func (discardStateStore) SaveSnapshot(string, proto.Message) error { return nil }
func (discardStateStore) LoadSnapshot(string, proto.Message) error { return errord.ErrNotFound }

type trackingPool struct {
	name          ResourceName
	maxSize       int
	maxCacheSize  int
	resourceCount int
	usingCount    int
	addCalls      int
}

func (p *trackingPool) Allocate(AllocateOption) (Resource, error) { return nil, nil }
func (p *trackingPool) Recycle(string) error                      { return nil }
func (p *trackingPool) Status() ([]string, []string)              { return nil, nil }
func (p *trackingPool) ShutDown() error                           { return nil }
func (p *trackingPool) ResourceName() ResourceName                { return p.name }
func (p *trackingPool) Add(num int) int {
	p.addCalls++
	p.resourceCount += num
	return num
}
func (p *trackingPool) Del(num int)         { p.resourceCount -= num }
func (p *trackingPool) CacheNum() int       { return p.resourceCount }
func (p *trackingPool) UsingNum() int       { return p.usingCount }
func (p *trackingPool) MaxSizeLimit() int   { return p.maxSize }
func (p *trackingPool) CacheSizeLimit() int { return p.maxCacheSize }

type warmPoolStubCgroupDriver struct {
	createCalls int
	processes   []int
	removeCalls int
	removeErr   error
}

func (d *warmPoolStubCgroupDriver) Mode() string            { return os2.CgroupModeV2 }
func (d *warmPoolStubCgroupDriver) EnsureRoot(string) error { return nil }
func (d *warmPoolStubCgroupDriver) ResolveRoot(rootName string) (string, error) {
	return "/" + rootName, nil
}

func (d *warmPoolStubCgroupDriver) Create(group string, resources *spec.LinuxResources) (os2.Cgroup, error) {
	d.createCalls++
	return &warmPoolStubCgroup{}, nil
}

func (d *warmPoolStubCgroupDriver) Load(group string) (os2.Cgroup, error) {
	return &warmPoolStubCgroup{processes: append([]int(nil), d.processes...)}, nil
}
func (d *warmPoolStubCgroupDriver) ExistingGroups(rootName string) ([]string, error) {
	return nil, nil
}
func (d *warmPoolStubCgroupDriver) Remove(group string) error {
	d.removeCalls++
	return d.removeErr
}
func (d *warmPoolStubCgroupDriver) LocalCPUCount() (int, error) { return 1, nil }

type warmPoolStubCgroup struct {
	processes []int
}

type gcRetirementMemoryStub struct {
	observation *hostlinux.CgroupMemoryObservation
}

func (s *gcRetirementMemoryStub) InspectParent(string) (*hostlinux.CgroupMemoryDomain, error) {
	return &hostlinux.CgroupMemoryDomain{}, nil
}

func (s *gcRetirementMemoryStub) ReadObservation(string) (*hostlinux.CgroupMemoryObservation, error) {
	return s.observation, nil
}

func (c *warmPoolStubCgroup) Update(resources *spec.LinuxResources) error { return nil }
func (c *warmPoolStubCgroup) Delete() error                               { return nil }
func (c *warmPoolStubCgroup) Stats() (*os2.CgroupStats, error)            { return &os2.CgroupStats{}, nil }
func (c *warmPoolStubCgroup) AddProc(pid uint64) error                    { return nil }
func (c *warmPoolStubCgroup) Processes(recursive bool) ([]int, error) {
	return append([]int(nil), c.processes...), nil
}

func resourcePoolAttrs(resource string) map[string]string {
	return map[string]string{sdkobs.AttrResource: resource}
}

func resourcePoolAllocateAttrs(resource, result string) map[string]string {
	return map[string]string{
		sdkobs.AttrResource: resource,
		sdkobs.AttrResult:   result,
	}
}

func TestPoolControllerCoalescesPendingRequests(t *testing.T) {
	controller := newPoolController(&trackingPool{name: "mock-coalesce", maxSize: 4, maxCacheSize: 2}, time.Second)
	controller.request(ResourcePoolTriggerLowWatermark)
	controller.request(ResourcePoolTriggerAllocationMiss)
	if got := len(controller.triggerC); got != 1 {
		t.Fatalf("pending trigger queue len = %d, want 1", got)
	}
}

func TestCgroupAllocateDisabledDevDoesNotRequireMemcgCapacitySample(t *testing.T) {
	driver := &warmPoolStubCgroupDriver{}
	manager := &CgroupManager{
		size: 2, cacheSize: 1, rootName: "/sandbox",
		usingID: cmap.New[struct{}](), idleID: queue.New(""), leases: cmap.New[*apipb.CgroupLease](),
		cgroups: cmap.New[struct{}](), gcQueue: queue.New(""), generator: truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		cgroupDriver: driver, db: discardStateStore{}, memoryAdmissionRequired: false,
	}
	resource, err := manager.Allocate(AllocateOption{ContainerID: "dev-allocation", MemoryRequestBytes: 4 << 30})
	if err != nil {
		t.Fatalf("Allocate() in disabled_dev mode error = %v", err)
	}
	if resource == nil || resource.ToString() == "" {
		t.Fatal("Allocate() in disabled_dev mode returned no cgroup")
	}
}

func TestReconcilePoolHonorsBoundsAndRecordsState(t *testing.T) {
	metrics.ResetForTest()

	pool := &trackingPool{
		name:         "mock-bounds",
		maxSize:      3,
		maxCacheSize: 5,
		usingCount:   2,
	}

	outcome := reconcilePool(pool)
	if !outcome.record {
		t.Fatal("reconcilePool() did not record a refill outcome")
	}
	if outcome.result != resourcePoolRefillOK {
		t.Fatalf("reconcilePool() result = %q, want %q", outcome.result, resourcePoolRefillOK)
	}
	if pool.resourceCount != 1 {
		t.Fatalf("resourceCount = %d, want 1", pool.resourceCount)
	}
	if pool.addCalls != 1 {
		t.Fatalf("addCalls = %d, want 1", pool.addCalls)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricResourcePoolIdleCurrent, resourcePoolAttrs("mock-bounds")); got != 1 {
		t.Fatalf("resource_pool_idle_current = %v, want 1", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricResourcePoolUsingCurrent, resourcePoolAttrs("mock-bounds")); got != 2 {
		t.Fatalf("resource_pool_using_current = %v, want 2", got)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricResourcePoolTargetCurrent, resourcePoolAttrs("mock-bounds")); got != 5 {
		t.Fatalf("resource_pool_target_current = %v, want 5", got)
	}
}

func TestCgroupManagerAllocateUsesIdlePool(t *testing.T) {
	metrics.ResetForTest()

	driver := &warmPoolStubCgroupDriver{}
	manager := &CgroupManager{
		size:         2,
		cacheSize:    1,
		rootName:     "sandbox",
		usingID:      cmap.New[struct{}](),
		idleID:       queue.New(""),
		cgroups:      cmap.New[struct{}](),
		generator:    truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		leases:       cmap.New[*apipb.CgroupLease](),
		gcQueue:      queue.New(""),
		cgroupDriver: driver,
		db:           discardStateStore{},
	}
	manager.idleID.Push("/sandbox/existing")
	manager.cgroups.Set("/sandbox/existing", struct{}{})
	manager.leases.Set("/sandbox/existing", &apipb.CgroupLease{CgroupID: "/sandbox/existing", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE})

	attrs := resourcePoolAllocateAttrs("cgroup", ResourcePoolAllocateHit)
	before := metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs)
	resource, err := manager.Allocate(AllocateOption{ContainerID: "idle-hit"})
	assert.NoError(t, err)
	assert.Equal(t, "/sandbox/existing", resource.ToString())
	assert.Equal(t, 0, driver.createCalls)
	assert.Equal(t, before+1, metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs))
}

func TestCgroupManagerMemoryAdmissionRequiresFreshCommitmentAndUsageHeadroom(t *testing.T) {
	newManager := func(snapshot MemoryCapacitySnapshot) *CgroupManager {
		manager := &CgroupManager{
			size: 2, cacheSize: 1, rootName: "sandbox",
			usingID: cmap.New[struct{}](), idleID: queue.New(""), cgroups: cmap.New[struct{}](), leases: cmap.New[*apipb.CgroupLease](),
			generator: truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
			gcQueue:   queue.New(""), cgroupDriver: &warmPoolStubCgroupDriver{}, db: discardStateStore{}, memoryCapacity: snapshot,
			memoryAdmissionRequired: true,
		}
		manager.idleID.Push("/sandbox/idle")
		manager.cgroups.Set("/sandbox/idle", struct{}{})
		manager.leases.Set("/sandbox/idle", &apipb.CgroupLease{CgroupID: "/sandbox/idle", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE})
		return manager
	}
	base := MemoryCapacitySnapshot{EffectiveAllocatableBytes: 1024, CapacityIdentity: "boot:mount:root", SampledAt: time.Now().UTC()}
	stale := base
	stale.SampledAt = time.Now().Add(-memoryCapacityFreshness - time.Second)
	if _, err := newManager(stale).Allocate(AllocateOption{ContainerID: "stale", MemoryRequestBytes: 1}); err == nil {
		t.Fatal("Allocate() accepted stale memory capacity")
	}
	hot := base
	hot.SandboxCurrentBytes = 900
	if _, err := newManager(hot).Allocate(AllocateOption{ContainerID: "hot", MemoryRequestBytes: 128}); !errors.Is(err, errord.ErrResourceExhausted) {
		t.Fatalf("Allocate() current headroom error = %v, want resource exhausted", err)
	}
	manager := newManager(base)
	manager.leases.Set("/sandbox/existing", &apipb.CgroupLease{
		CgroupID: "/sandbox/existing", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "existing", MemoryRequestBytes: 900, AssignedAtUnixNano: 1,
	})
	if _, err := manager.Allocate(AllocateOption{ContainerID: "committed", MemoryRequestBytes: 128}); !errors.Is(err, errord.ErrResourceExhausted) {
		t.Fatalf("Allocate() commitment error = %v, want resource exhausted", err)
	}
}

func TestMemoryCommitmentSaturatesInsteadOfWrapping(t *testing.T) {
	manager := &CgroupManager{leases: cmap.New[*apipb.CgroupLease]()}
	manager.leases.Set("/sandbox/a", &apipb.CgroupLease{
		CgroupID: "/sandbox/a", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED,
		AllocationID: "a", MemoryRequestBytes: 1<<62 + 1, AssignedAtUnixNano: 1,
	})
	manager.leases.Set("/sandbox/b", &apipb.CgroupLease{
		CgroupID: "/sandbox/b", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		AllocationID: "b", MemoryRequestBytes: 1<<62 + 1, RetiringAtUnixNano: 1,
	})
	commitment := manager.MemoryCommitment()
	if commitment.CommittedBytes != math.MaxInt64 || commitment.CleanupDebtBytes != 1<<62+1 {
		t.Fatalf("MemoryCommitment() = %+v", commitment)
	}
}

func TestConvergeRetiringCgroupChargesOrphanBeforeRemainingProcessRetry(t *testing.T) {
	id := "/sandbox/orphan"
	driver := &warmPoolStubCgroupDriver{processes: []int{123}}
	manager := &CgroupManager{
		leases: cmap.New[*apipb.CgroupLease](), cgroups: cmap.New[struct{}](),
		cgroupDriver: driver, db: discardStateStore{},
		retirementMemory: &gcRetirementMemoryStub{
			observation: &hostlinux.CgroupMemoryObservation{CurrentBytes: 4096, Stat: map[string]int64{}},
		},
	}
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		RetiringAtUnixNano: time.Now().UTC().UnixNano(), LastCleanupError: "cgroup has no durable ownership record",
	})
	manager.cgroups.Set(id, struct{}{})

	if err := manager.convergeRetiringCgroup(id); err == nil {
		t.Fatal("convergeRetiringCgroup() accepted an orphan cgroup with remaining processes")
	}
	lease, _ := manager.leases.Get(id)
	if got := lease.GetCurrentChargedBytes(); got != 4096 {
		t.Fatalf("orphan cleanup charge = %d, want 4096", got)
	}
	commitment := manager.MemoryCommitment()
	if commitment.CommittedBytes != 4096 || commitment.CleanupDebtBytes != 4096 {
		t.Fatalf("orphan cleanup commitment = %+v, want 4096 bytes of debt", commitment)
	}
}

func TestConvergeRetiringCgroupRemovesEmptyDomainWithResidualWritebackWithoutMemoryReclaim(t *testing.T) {
	id := "/sandbox/without-memory-reclaim"
	driver := &warmPoolStubCgroupDriver{}
	memory := &gcRetirementMemoryStub{
		observation: &hostlinux.CgroupMemoryObservation{
			CurrentBytes: 4096,
			Stat:         map[string]int64{"file_dirty": 135168, "file_writeback": 4096},
		},
	}
	manager := &CgroupManager{
		leases: cmap.New[*apipb.CgroupLease](), cgroups: cmap.New[struct{}](),
		cgroupDriver: driver, db: discardStateStore{}, retirementMemory: memory,
	}
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		AllocationID: "self-test", MemoryRequestBytes: 1024, RetiringAtUnixNano: time.Now().UTC().UnixNano(),
	})
	manager.cgroups.Set(id, struct{}{})

	if err := manager.convergeRetiringCgroup(id); err != nil {
		t.Fatalf("convergeRetiringCgroup() error = %v", err)
	}
	if driver.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", driver.removeCalls)
	}
	lease, _ := manager.leases.Get(id)
	if lease.GetCurrentChargedBytes() != 4096 {
		t.Fatalf("retiring lease = %+v", lease)
	}
}

func TestConvergeRetiringCgroupKeepsDebtWhenRemovalFailsWithoutMemoryReclaim(t *testing.T) {
	id := "/sandbox/without-memory-reclaim-busy"
	removeErr := errors.New("cgroup is still busy")
	driver := &warmPoolStubCgroupDriver{removeErr: removeErr}
	memory := &gcRetirementMemoryStub{
		observation: &hostlinux.CgroupMemoryObservation{
			CurrentBytes: 4096,
			Stat:         map[string]int64{"file_dirty": 135168, "file_writeback": 4096},
		},
	}
	manager := &CgroupManager{
		leases: cmap.New[*apipb.CgroupLease](), cgroups: cmap.New[struct{}](),
		cgroupDriver: driver, db: discardStateStore{}, retirementMemory: memory,
	}
	manager.leases.Set(id, &apipb.CgroupLease{
		CgroupID: id, State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		AllocationID: "self-test", MemoryRequestBytes: 1024, RetiringAtUnixNano: time.Now().UTC().UnixNano(),
	})
	manager.cgroups.Set(id, struct{}{})

	err := manager.convergeRetiringCgroup(id)
	if !errors.Is(err, removeErr) {
		t.Fatalf("convergeRetiringCgroup() error = %v, want removal failure", err)
	}
	if driver.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", driver.removeCalls)
	}
	lease, ok := manager.leases.Get(id)
	if !ok || lease.GetState() != apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING || lease.GetCurrentChargedBytes() != 4096 {
		t.Fatalf("failed removal released cleanup debt: lease=%+v present=%v", lease, ok)
	}
}

func TestMissingKernelLeaseOnlyConvergesWhenUnassigned(t *testing.T) {
	if missingKernelLeaseConverged(apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED) {
		t.Fatal("assigned cgroup ownership was treated as cleanup-complete before runtime reconciliation")
	}
	for _, state := range []apipb.CgroupLifecycleState{
		apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_IDLE,
		apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
	} {
		if !missingKernelLeaseConverged(state) {
			t.Fatalf("missing kernel lease state %s did not converge", state)
		}
	}
}

func TestCgroupManagerAllocateReturnsExhaustedWhenAtCapacity(t *testing.T) {
	metrics.ResetForTest()

	driver := &warmPoolStubCgroupDriver{}
	manager := &CgroupManager{
		size:         1,
		cacheSize:    1,
		rootName:     "sandbox",
		usingID:      cmap.New[struct{}](),
		idleID:       queue.New(""),
		cgroups:      cmap.New[struct{}](),
		generator:    truncindex.NewFixLenGenerator(12, nil, truncindex.PrefixModifier("/sandbox/")),
		leases:       cmap.New[*apipb.CgroupLease](),
		gcQueue:      queue.New(""),
		cgroupDriver: driver,
		db:           discardStateStore{},
	}
	manager.cgroups.Set("/sandbox/in-use", struct{}{})
	manager.usingID.Set("/sandbox/in-use", struct{}{})
	manager.leases.Set("/sandbox/in-use", &apipb.CgroupLease{CgroupID: "/sandbox/in-use", State: apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_ASSIGNED, AllocationID: "full", AssignedAtUnixNano: 1})

	attrs := resourcePoolAllocateAttrs("cgroup", ResourcePoolAllocateExhausted)
	before := metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs)
	_, err := manager.Allocate(AllocateOption{ContainerID: "full"})
	if !errors.Is(err, errord.ErrResourceExhausted) {
		t.Fatalf("Allocate() error = %v, want %v", err, errord.ErrResourceExhausted)
	}
	assert.Equal(t, before+1, metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs))
}

func TestInterfaceManagerAllocateMissCreatesSynchronously(t *testing.T) {
	metrics.ResetForTest()

	var resetIP string
	manager := &InterfaceManager{
		size:            2,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		BridgeIp:        net.ParseIP("172.17.0.1"),
		mask:            net.CIDRMask(24, 32),
		createDeviceFunc: func(string) error {
			return nil
		},
		lookupDeviceFunc: func(name string) (*net.Interface, error) {
			return &net.Interface{Name: name}, nil
		},
		deleteNeighborFunc: func(ip net.IP) error {
			resetIP = ip.String()
			return nil
		},
	}
	manager.idleIp.Push("172.17.0.2")

	attrs := resourcePoolAllocateAttrs("interface", ResourcePoolAllocateMissSyncCreate)
	before := metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs)
	resource, err := manager.Allocate(AllocateOption{ContainerID: "sync-create"})
	assert.NoError(t, err)
	assert.NotNil(t, resource)
	netResource, ok := resource.(*NetResource)
	if !ok {
		t.Fatalf("resource type = %T, want *NetResource", resource)
	}
	wantInterfaceName, _ := ipToVeth("172.17.0.2")
	if netResource.Interface == nil || netResource.Interface.Name != wantInterfaceName {
		t.Fatalf("interface = %#v", netResource.Interface)
	}
	assert.Equal(t, 1, manager.UsingNum())
	assert.Equal(t, "172.17.0.2", resetIP)
	assert.Equal(t, before+1, metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs))
}

func TestInterfaceManagerAllocateHitClearsNeighborBeforeUse(t *testing.T) {
	var resetIP string
	manager := &InterfaceManager{
		cacheSize:       1,
		interfaces:      queue.New(""),
		idleIp:          queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		validateDeviceFunc: func(*NetResource) error {
			return nil
		},
		deleteNeighborFunc: func(ip net.IP) error {
			resetIP = ip.String()
			return nil
		},
	}
	resource := &NetResource{
		Interface: &net.Interface{Name: "veth-test"},
		Ip:        net.ParseIP("172.17.0.3"),
	}
	manager.interfaces.Push(resource.ToString())

	allocated, err := manager.Allocate(AllocateOption{})

	assert.NoError(t, err)
	assert.Equal(t, "172.17.0.3", resetIP)
	assert.Equal(t, resource.ToString(), allocated.ToString())
	assert.True(t, manager.usingInterfaces.Has(resource.ToString()))
}

func TestInterfaceManagerAllocateReturnsExhaustedWhenAtCapacity(t *testing.T) {
	metrics.ResetForTest()

	manager := &InterfaceManager{
		size:            1,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
	}
	manager.usingInterfaces.Set("busy", struct{}{})

	attrs := resourcePoolAllocateAttrs("interface", ResourcePoolAllocateExhausted)
	before := metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs)
	_, err := manager.Allocate(AllocateOption{ContainerID: "full"})
	if !errors.Is(err, errord.ErrResourceExhausted) {
		t.Fatalf("Allocate() error = %v, want %v", err, errord.ErrResourceExhausted)
	}
	assert.Equal(t, before+1, metrics.CounterValueForTest(metrics.MetricResourcePoolAllocateTotal, attrs))
}

func TestInterfaceManagerAllocateWaitsForInFlightPoolBuild(t *testing.T) {
	manager := newBlockingInterfacePoolManager()
	defer manager.releaseLookup()
	buildDone := make(chan int, 1)
	go func() {
		buildDone <- manager.Add(1)
	}()
	<-manager.lookupStarted

	waitContext := newWaitObservedContext(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Allocate(AllocateOption{Context: waitContext})
		result <- err
	}()

	select {
	case <-waitContext.waitObserved:
	case err := <-result:
		t.Fatalf("Allocate() returned before waiting for the in-flight build: %v", err)
	}
	manager.releaseLookup()
	assert.Equal(t, 1, <-buildDone)
	assert.NoError(t, <-result)
	assert.Equal(t, 1, manager.UsingNum())
	assert.Equal(t, int64(1), manager.activeSlots.Load())
}

func TestInterfaceManagerWaitForBuildObservesCompletedGeneration(t *testing.T) {
	manager := newBlockingInterfacePoolManager()
	defer manager.releaseLookup()

	observedGeneration := manager.currentBuildGeneration()
	buildDone := make(chan int, 1)
	go func() {
		buildDone <- manager.Add(1)
	}()
	<-manager.lookupStarted
	manager.releaseLookup()
	assert.Equal(t, 1, <-buildDone)

	assert.NoError(t, manager.waitForBuild(context.Background(), observedGeneration))
}

func TestInterfaceManagerAllocateCancelsWhileWaitingForPoolBuild(t *testing.T) {
	manager := newBlockingInterfacePoolManager()
	defer manager.releaseLookup()
	buildDone := make(chan int, 1)
	go func() {
		buildDone <- manager.Add(1)
	}()
	<-manager.lookupStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := manager.Allocate(AllocateOption{Context: ctx})
	assert.ErrorIs(t, err, context.Canceled)

	manager.releaseLookup()
	assert.Equal(t, 1, <-buildDone)
}

type waitObservedContext struct {
	context.Context
	waitObserved chan struct{}
	once         sync.Once
}

func newWaitObservedContext(ctx context.Context) *waitObservedContext {
	return &waitObservedContext{
		Context:      ctx,
		waitObserved: make(chan struct{}),
	}
}

func (c *waitObservedContext) Done() <-chan struct{} {
	// waitForBuild evaluates Done only after it has captured buildChanged. This
	// signal lets the test release the builder without depending on scheduling.
	c.once.Do(func() { close(c.waitObserved) })
	return c.Context.Done()
}

type blockingInterfacePoolManager struct {
	*InterfaceManager
	lookupStarted  chan struct{}
	continueLookup chan struct{}
	releaseOnce    sync.Once
}

func (m *blockingInterfacePoolManager) releaseLookup() {
	m.releaseOnce.Do(func() { close(m.continueLookup) })
}

func newBlockingInterfacePoolManager() *blockingInterfacePoolManager {
	lookupStarted := make(chan struct{})
	continueLookup := make(chan struct{})
	manager := &InterfaceManager{
		size:            1,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		BridgeIp:        net.ParseIP("172.17.0.1"),
		mask:            net.CIDRMask(24, 32),
		createDeviceFunc: func(string) error {
			return nil
		},
		validateDeviceFunc: func(*NetResource) error {
			return nil
		},
	}
	manager.lookupDeviceFunc = func(name string) (*net.Interface, error) {
		close(lookupStarted)
		<-continueLookup
		return &net.Interface{Name: name}, nil
	}
	manager.idleIp.Push("172.17.0.2")
	return &blockingInterfacePoolManager{
		InterfaceManager: manager,
		lookupStarted:    lookupStarted,
		continueLookup:   continueLookup,
	}
}

func TestInterfaceManagerConcurrentMissesRespectCapacity(t *testing.T) {
	manager := &InterfaceManager{
		size:            8,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		BridgeIp:        net.ParseIP("172.17.0.1"),
		mask:            net.CIDRMask(24, 32),
	}
	for i := 2; i < 34; i++ {
		manager.idleIp.Push(net.IPv4(172, 17, 0, byte(i)).String())
	}

	var creates atomic.Int64
	manager.createDeviceFunc = func(string) error {
		creates.Add(1)
		time.Sleep(10 * time.Millisecond)
		return nil
	}
	manager.lookupDeviceFunc = func(name string) (*net.Interface, error) {
		return &net.Interface{Name: name}, nil
	}

	const attempts = 32
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Allocate(AllocateOption{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	succeeded := 0
	exhausted := 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, errord.ErrResourceExhausted):
			exhausted++
		default:
			t.Fatalf("Allocate() error = %v", err)
		}
	}
	assert.Equal(t, 8, succeeded)
	assert.Equal(t, attempts-8, exhausted)
	assert.Equal(t, int64(8), creates.Load())
	assert.Equal(t, int64(8), manager.activeSlots.Load())
}

func TestInterfaceManagerFailedBuildReleasesCapacitySlot(t *testing.T) {
	manager := &InterfaceManager{
		size:            1,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		BridgeIp:        net.ParseIP("172.17.0.1"),
		mask:            net.CIDRMask(24, 32),
		createDeviceFunc: func(string) error {
			return errors.New("create failed")
		},
	}
	manager.idleIp.Push("172.17.0.2")

	if _, err := manager.Allocate(AllocateOption{}); err == nil {
		t.Fatal("Allocate() error = nil, want create failure")
	}
	assert.Equal(t, int64(0), manager.activeSlots.Load())
	assert.Equal(t, 1, manager.idleIp.Length())

	manager.createDeviceFunc = func(string) error { return nil }
	manager.lookupDeviceFunc = func(name string) (*net.Interface, error) {
		return &net.Interface{Name: name}, nil
	}
	if _, err := manager.Allocate(AllocateOption{}); err != nil {
		t.Fatalf("Allocate() after rollback error = %v", err)
	}
	assert.Equal(t, int64(1), manager.activeSlots.Load())
}

func TestInterfaceManagerQuarantinesSlotWhenFailedBuildCannotCleanUp(t *testing.T) {
	manager := &InterfaceManager{
		size:            1,
		cacheSize:       1,
		idleIp:          queue.New(""),
		interfaces:      queue.New(""),
		usingInterfaces: cmap.New[struct{}](),
		BridgeIp:        net.ParseIP("172.17.0.1"),
		mask:            net.CIDRMask(24, 32),
		createDeviceFunc: func(string) error {
			return nil
		},
		lookupDeviceFunc: func(string) (*net.Interface, error) {
			return nil, errors.New("lookup failed")
		},
		destroyDeviceFunc: func(net.Interface) error {
			return errors.New("cleanup failed")
		},
	}
	manager.idleIp.Push("172.17.0.2")

	if _, err := manager.Allocate(AllocateOption{}); err == nil {
		t.Fatal("Allocate() error = nil, want lookup and cleanup failure")
	}
	assert.Equal(t, int64(1), manager.activeSlots.Load())
	assert.Equal(t, 1, manager.UnavailableNum())
	assert.Equal(t, 0, manager.idleIp.Length())
	if _, err := manager.Allocate(AllocateOption{}); !errors.Is(err, errord.ErrResourceExhausted) {
		t.Fatalf("Allocate() after quarantine error = %v, want resource exhausted", err)
	}
}
