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
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
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

func inventoryContainer(id string, status container.Status) *container.Container {
	return &container.Container{
		Metadata: &runtimeapi.ContainerMetadata{ID: id},
		Status:   &fakeStatusStorage{status: status},
	}
}
