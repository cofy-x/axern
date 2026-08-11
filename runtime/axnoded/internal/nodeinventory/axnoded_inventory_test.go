package nodeinventory

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestCollectAxnodedInventoryIncludesRetentionHeat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires linux rootfs retention semantics")
	}

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(rootfsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	manager := langruntime.NewLanguageRuntimeManager()
	manager.ConfigureRetention(time.Minute, 1)
	fr := &runtimeapi.RuntimeTemplate{
		ID:      "inventory-retained",
		Sandbox: "runsc",
		Rootfs: &runtimeapi.RootfsConfig{
			Type:   runtimeapi.RootfsSrcType_LOCAL,
			Source: &runtimeapi.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sh"},
	}

	rootfsCfg, err := langruntime.RootfsConfigFromRuntimeTemplate(fr)
	if err != nil {
		t.Fatalf("RootfsConfigFromRuntimeTemplate() error = %v", err)
	}
	result, err := manager.AddLangRuntime(t.Context(), fr, rootfsCfg, true)
	if err != nil {
		t.Fatalf("AddLangRuntime() error = %v", err)
	}
	lr := result.Runtime
	lr.IncRef()
	lr.DecRef()

	source := NewAxnodedSource(AxnodedSourceOptions{
		Ready:        func() bool { return true },
		RuntimeCount: func() int { return 1 },
		Container: &fakeContainerManager{
			pools: map[string]PoolInventory{
				"cgroup":    {Capacity: 8},
				"interface": {Capacity: 8},
			},
		},
		LangRuntime: manager,
	})

	snapshot := NewSnapshot()
	source.collectAxnodedInventory(time.Now().UTC(), &snapshot)

	if got := snapshot.Heat.RetainedRuntimeCount; got != 1 {
		t.Fatalf("retained_runtime_count = %d, want 1", got)
	}
	if got := snapshot.Heat.RetainedRootfsCount; got != 1 {
		t.Fatalf("retained_rootfs_count = %d, want 1", got)
	}
	if len(snapshot.Heat.Locality) != 1 {
		t.Fatalf("locality entries = %d, want 1", len(snapshot.Heat.Locality))
	}
	if snapshot.Heat.Locality[0].Key != "local:"+rootfsDir {
		t.Fatalf("locality key = %q, want %q", snapshot.Heat.Locality[0].Key, "local:"+rootfsDir)
	}
	if snapshot.Heat.Locality[0].RetainedRuntimeCount != 1 {
		t.Fatalf("retained_runtime_count = %d, want 1", snapshot.Heat.Locality[0].RetainedRuntimeCount)
	}
	if snapshot.Heat.Locality[0].RetainedRootfsCount != 1 {
		t.Fatalf("retained_rootfs_count = %d, want 1", snapshot.Heat.Locality[0].RetainedRootfsCount)
	}
}

func TestCollectAxnodedInventoryAllowsConfiguredDisabledPool(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		Ready:                 func() bool { return true },
		RuntimeCount:          func() int { return 1 },
		Container:             &fakeContainerManager{pools: map[string]PoolInventory{"interface": {Idle: 3, Capacity: 8}}},
		DisabledResourcePools: []resources.ResourceName{resources.CgroupResourceName},
	})

	snapshot := NewSnapshot()
	if ready := source.collectAxnodedInventory(time.Now().UTC(), &snapshot); !ready {
		t.Fatalf("collectAxnodedInventory() ready = false, want true")
	}
	if snapshot.Pools.Cgroup != (PoolInventory{}) {
		t.Fatalf("cgroup pool = %+v, want zero value", snapshot.Pools.Cgroup)
	}
	if got := snapshot.Pools.Interface.Capacity; got != 8 {
		t.Fatalf("interface capacity = %d, want 8", got)
	}
	if got := snapshot.Pools.RuntimeSlots.Capacity - snapshot.Pools.RuntimeSlots.Unavailable; got != 8 {
		t.Fatalf("effective runtime slot capacity = %d, want 8", got)
	}
	if got := snapshot.Pools.RuntimeSlots.Idle; got != 3 {
		t.Fatalf("warm runtime slots = %d, want 3", got)
	}
}

func TestCollectAxnodedInventoryRequiresConfiguredPool(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		Ready:               func() bool { return true },
		RuntimeCount:        func() int { return 1 },
		Container:           &fakeContainerManager{pools: map[string]PoolInventory{"interface": {Capacity: 8}}},
		RuntimeSlotCapacity: 8,
	})

	snapshot := NewSnapshot()
	if ready := source.collectAxnodedInventory(time.Now().UTC(), &snapshot); ready {
		t.Fatal("collectAxnodedInventory() ready = true with missing configured cgroup pool")
	}
	if snapshot.Components.Axnoded.Status != StatusError {
		t.Fatalf("axnoded status = %q, want %q", snapshot.Components.Axnoded.Status, StatusError)
	}
}

func TestRuntimeSlotInventoryAggregatesEnabledPoolConstraints(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{RuntimeSlotCapacity: 64})
	slots := source.runtimeSlotInventory(3, PoolsInventory{
		Cgroup:    PoolInventory{Using: 2, Idle: 6, Capacity: 64},
		Interface: PoolInventory{Using: 4, Idle: 5, Capacity: 64, Unavailable: 2},
	})
	want := (PoolInventory{Using: 4, Idle: 5, Capacity: 64, Unavailable: 2})
	if slots != want {
		t.Fatalf("runtimeSlotInventory() = %+v, want %+v", slots, want)
	}
}

func TestNewAxnodedSourceCapsRuntimeSlotsAtContainerLimit(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		RuntimeSlotCapacity:   container.MaxContainerNum + 1,
		DisabledResourcePools: []resources.ResourceName{resources.CgroupResourceName, resources.InterfaceResourceName},
	})
	slots := source.runtimeSlotInventory(0, PoolsInventory{})
	if slots.Capacity != container.MaxContainerNum {
		t.Fatalf("runtime slot capacity = %d, want container limit %d", slots.Capacity, container.MaxContainerNum)
	}
}

func TestCollectAxnodedInventoryResourceCommitmentUsesRequests(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		Ready:        func() bool { return true },
		RuntimeCount: func() int { return 1 },
		Container: &fakeContainerManager{
			list: []*container.Container{
				inventoryContainer("request-only", container.Status{
					StartedAt: "2026-05-09T00:00:00Z",
					ResourceSpec: &commonv1.ResourceSpec{
						Requests: &commonv1.ResourceQuantity{
							CpuMilli:    250,
							MemoryBytes: 128 * 1024 * 1024,
						},
					},
					LinuxResources: &runtimeapi.LinuxContainerResources{
						CpuShares: 256,
					},
				}),
				inventoryContainer("limit-only", container.Status{
					StartedAt: "2026-05-09T00:00:00Z",
					LinuxResources: &runtimeapi.LinuxContainerResources{
						CpuPeriod:          100000,
						CpuQuota:           50000,
						MemoryLimitInBytes: 256 * 1024 * 1024,
					},
				}),
				inventoryContainer("unbounded", container.Status{
					StartedAt: "2026-05-09T00:00:00Z",
				}),
				inventoryContainer("exited-ignored", container.Status{
					StartedAt:  "2026-05-09T00:00:00Z",
					FinishedAt: "2026-05-09T00:00:01Z",
					ResourceSpec: &commonv1.ResourceSpec{
						Requests: &commonv1.ResourceQuantity{
							CpuMilli:    900,
							MemoryBytes: 900,
						},
					},
				}),
			},
			pools: map[string]PoolInventory{
				"cgroup":    {Capacity: 8},
				"interface": {Capacity: 8},
			},
			runtimeCgroup: map[string]string{
				"request-only": "/sandbox/request-only",
				"limit-only":   "/sandbox/limit-only",
				"unbounded":    "/sandbox/unbounded",
			},
		},
		CgroupDriver: &fakeCgroupDriver{stats: map[string]*os2.CgroupStats{
			"/sandbox/request-only": {},
			"/sandbox/limit-only":   {},
			"/sandbox/unbounded":    {},
		}},
	})

	snapshot := NewSnapshot()
	source.collectAxnodedInventory(time.Now().UTC(), &snapshot)

	if got, want := snapshot.Components.Axnoded.RunningContainers, 3; got != want {
		t.Fatalf("running_containers = %d, want %d", got, want)
	}
	if got, want := snapshot.Resources.CPU.AxnodedCommittedMilli, int64(750); got != want {
		t.Fatalf("axnoded_committed_milli = %d, want %d", got, want)
	}
	if got, want := snapshot.Resources.Memory.AxnodedCommittedBytes, int64(384*1024*1024); got != want {
		t.Fatalf("axnoded_committed_bytes = %d, want %d", got, want)
	}
	if got, want := snapshot.Resources.CPU.AxnodedUnboundedCount, int64(1); got != want {
		t.Fatalf("cpu_unbounded_count = %d, want %d", got, want)
	}
	if got, want := snapshot.Resources.Memory.AxnodedUnboundedCount, int64(1); got != want {
		t.Fatalf("memory_unbounded_count = %d, want %d", got, want)
	}
}

func TestCollectAxnodedInventoryDisabledDevDoesNotFabricateCgroupUsage(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		Ready:               func() bool { return true },
		RuntimeCount:        func() int { return 1 },
		MemoryBudgetEnabled: true,
		// No CgroupDriver is intentional: disabled_dev has no allocation-owned
		// cgroup from which usage could be attributed safely.
		MemoryCgroupEnforced: false,
		Container: &fakeContainerManager{
			list: []*container.Container{
				inventoryContainer("disabled-dev", container.Status{
					StartedAt: "2026-08-11T00:00:00Z",
					ResourceSpec: &commonv1.ResourceSpec{
						Requests: &commonv1.ResourceQuantity{MemoryBytes: 128 * 1024 * 1024},
					},
				}),
			},
			pools: map[string]PoolInventory{
				"cgroup":    {Capacity: 8},
				"interface": {Capacity: 8},
			},
			runtimeCgroup: map[string]string{"disabled-dev": "/"},
		},
	})

	snapshot := NewSnapshot()
	if ready := source.collectAxnodedInventory(time.Now().UTC(), &snapshot); !ready {
		t.Fatal("collectAxnodedInventory() ready = false in disabled_dev")
	}
	if got := snapshot.Components.Axnoded.Status; got != StatusReady {
		t.Fatalf("axnoded status = %q, want %q", got, StatusReady)
	}
	if got, want := snapshot.Resources.Memory.AxnodedCommittedBytes, int64(128*1024*1024); got != want {
		t.Fatalf("committed memory = %d, want %d", got, want)
	}
	if got := snapshot.Resources.Memory.AxnodedUsedBytes; got != 0 {
		t.Fatalf("attributed memory usage = %d, want unavailable zero projection", got)
	}
	if got := len(snapshot.AllocationMemoryObservations); got != 0 {
		t.Fatalf("allocation memory observations = %d, want 0", got)
	}
}

func inventoryContainer(id string, status container.Status) *container.Container {
	return &container.Container{
		Metadata: &runtimeapi.ContainerMetadata{ID: id},
		Status:   &fakeStatusStorage{status: status},
	}
}

func TestMemoryObservationFromKernelPreservesRetiringOwnership(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	observation := memoryObservationFromKernel(
		"alloc-retiring", 4, 512, 1024, "runsc", nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING, 9, now,
		&hostlinux.CgroupMemoryDomain{BootID: "boot", MountIdentity: "mount", ParentInode: 11, LeafInode: 12},
		&hostlinux.CgroupMemoryObservation{
			CurrentBytes: 700, PeakBytes: 900, PeakAvailable: true, Stat: map[string]int64{"anon": 100, "file": 500},
			Events: map[string]uint64{"oom": 1}, PSIAvailable: true, PSISomeAvg10: 0.5, PSISomeTotal: 42,
		},
		true, false,
	)
	if observation.GetAllocationID() != "alloc-retiring" || observation.GetAttempt() != 4 || observation.GetRevision() != 9 ||
		observation.GetCleanupState() != nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING || observation.GetCurrentBytes() != 700 || observation.GetRuntime() != "runsc" ||
		observation.GetCgroupIdentity() != "boot=boot:mount:11:12" || !observation.GetParentControlsVerified() || observation.GetLeafControlsVerified() ||
		!observation.GetPsiAvailable() || observation.GetPsiSomeAvg10() != 0.5 || observation.GetPsiSomeTotalUsec() != 42 {
		t.Fatalf("memoryObservationFromKernel() = %+v", observation)
	}
}

func TestMemoryObservationFromKernelRepresentsUnlimitedSandboxWithoutHardControlClaim(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	observation := memoryObservationFromKernel(
		"alloc-unlimited", 2, 512, 0, "runc", nodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_ASSIGNED, 10, now,
		&hostlinux.CgroupMemoryDomain{BootID: "boot", MountIdentity: "mount", ParentInode: 21, LeafInode: 22, LimitBytes: -1, SwapMaxBytes: -1},
		&hostlinux.CgroupMemoryObservation{CurrentBytes: 700, PeakBytes: 900, PeakAvailable: true, SwapCurrent: 12},
		false, false,
	)
	if observation.GetLimitBytes() != 0 || observation.GetSwapCurrentBytes() != 12 || observation.GetParentControlsVerified() || observation.GetLeafControlsVerified() ||
		observation.GetCgroupIdentity() != "boot=boot:mount:21:22" {
		t.Fatalf("unlimited memory observation = %+v", observation)
	}
}
