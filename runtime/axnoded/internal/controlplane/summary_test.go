package controlplane

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestBuildNodeSummaryMapsInventorySnapshot(t *testing.T) {
	collectedAt := time.Date(2026, 4, 21, 7, 0, 0, 0, time.UTC)
	snapshot := nodeinventory.NewSnapshot()
	snapshot.Node.CollectedAt = collectedAt
	snapshot.Node.State = "draining"
	snapshot.Node.Labels = map[string]string{"zone": "us-east-1"}
	snapshot.Node.CapabilitySnapshot = &capabilityv1.CapabilitySnapshot{NodeInstanceID: "node-instance", Sequence: 7, SnapshotID: "snapshot-7"}
	snapshot.Node.Capacity = nodeinventory.NodeResourceQuantity{CpuMilli: 8000, MemoryBytes: 16 << 30}
	snapshot.Node.Allocatable = nodeinventory.NodeResourceQuantity{CpuMilli: 6000, MemoryBytes: 12 << 30}
	snapshot.Resources.CPU.AxnodedCommittedMilli = 1200
	snapshot.Resources.CPU.AxnodedUsedMilli = 450
	snapshot.Resources.CPU.AxnodedUnboundedCount = 2
	snapshot.Resources.Memory.AxnodedCommittedBytes = 1024
	snapshot.Resources.Memory.AxnodedUsedBytes = 512
	snapshot.Resources.Memory.AxnodedUnboundedCount = 1
	snapshot.Pools.Cgroup = nodeinventory.PoolInventory{Using: 2, Idle: 3, Capacity: 8}
	snapshot.Pools.Interface = nodeinventory.PoolInventory{Using: 1, Idle: 4, Capacity: 8, Unavailable: 2}
	snapshot.Pools.RuntimeSlots = nodeinventory.PoolInventory{Using: 2, Idle: 4, Capacity: 8, Unavailable: 2}
	snapshot.Components.Axnoded = nodeinventory.AxnodedComponentInventory{
		Status:               nodeinventory.StatusReady,
		Ready:                true,
		RunningContainers:    5,
		RunningAllocationIDs: []string{"alloc-b", "alloc-a"},
		ActiveAllocationIDs:  []string{"alloc-c", "alloc-b", "alloc-a"},
	}
	snapshot.Components.Imagemgr = nodeinventory.ImagemgrComponentInventory{Status: nodeinventory.StatusDegraded, Reachable: true, DaemonCount: 2, MountedImageCount: 3, ImportedImageCount: 4}
	snapshot.Components.Imagefsd = nodeinventory.ImagefsdComponentInventory{Status: nodeinventory.StatusReady, Reachable: true, ChunkDBPresent: true, ChunkCount: 9, ChunkDBUsedBytes: 2048, ChunkDBUsagePercent: 80.5}
	snapshot.Components.BPFNet = nodeinventory.BPFNetComponentInventory{Status: nodeinventory.StatusDisabled, Enabled: false, Ready: false, Mode: "iptables", NeedsSNATFallback: true, NeedsFullDNATFallback: true, NeedsLocalhostCompat: true}
	snapshot.Components.Volumed = nodeinventory.VolumedComponentInventory{Status: nodeinventory.StatusError, Reachable: true, PublishedVolumeCount: 2, LastReconcileAt: collectedAt.Add(time.Minute), LastReconcileError: "provider validation failed", LastReconcileRetainedCount: 3, LastReconcileUnpublishedCount: 1, LastReconcileActiveAllocationCount: 2, LastReconcileStaleAllocationCount: 1, LastReconcileInvalidVolumeCount: 1}
	snapshot.Storage = []nodeinventory.StorageInventoryEntry{
		{
			Target:          nodeinventory.StorageTargetAxnodedState,
			Path:            "/var/lib/axnoded",
			CapacityBytes:   1000,
			UsedBytes:       250,
			AvailableBytes:  700,
			InodesTotal:     100,
			InodesUsed:      10,
			InodesAvailable: 90,
			Collected:       true,
		},
		{
			Target:    nodeinventory.StorageTargetImageCache,
			Path:      "/var/lib/imagemgr",
			Collected: false,
			Error:     "statfs /var/lib/imagemgr: no such file or directory",
		},
	}
	snapshot.Heat.Locality = []nodeinventory.LocalityHeatEntry{{
		Key:                        "image:repo/app:latest",
		RootfsType:                 "image",
		MountType:                  "oci",
		Mounted:                    true,
		RetainedRuntimeCount:       2,
		RetainedRootfsCount:        3,
		RunningContainerCount:      4,
		NydusDaemonAlive:           true,
		ChunkDBTotalChunks:         11,
		ChunkDBUsedBytes:           4096,
		ChunkDBRecentAccessAgeSecs: 7,
		PeerHealthyCount:           5,
		PeerUnhealthyCount:         1,
		PeerHintedCount:            6,
	}}

	summary := BuildNodeSummary(snapshot)
	if summary.GetCollectedAt().AsTime() != collectedAt {
		t.Fatalf("CollectedAt = %v, want %v", summary.GetCollectedAt().AsTime(), collectedAt)
	}
	if summary.GetNodeState() != nodev1.NodeState_NODE_STATE_DRAINING {
		t.Fatalf("NodeState = %v, want DRAINING", summary.GetNodeState())
	}
	if summary.GetLabels()["zone"] != "us-east-1" {
		t.Fatalf("labels = %#v, want zone=us-east-1", summary.GetLabels())
	}
	if summary.GetCapabilitySnapshot().GetSnapshotID() != "snapshot-7" || summary.GetCapabilitySnapshot().GetSequence() != 7 {
		t.Fatalf("capability snapshot = %#v", summary.GetCapabilitySnapshot())
	}
	if summary.GetCapacity().GetCpuMilli() != 8000 || summary.GetAllocatable().GetMemoryBytes() != 12<<30 {
		t.Fatalf("unexpected capacity/allocatable = %#v %#v", summary.GetCapacity(), summary.GetAllocatable())
	}
	if summary.GetResources().GetAxnodedCommittedMilli() != 1200 || summary.GetResources().GetAxnodedCpuUnboundedCount() != 2 {
		t.Fatalf("unexpected cpu resources summary: %#v", summary.GetResources())
	}
	if summary.GetResources().GetAxnodedCommittedBytes() != 1024 || summary.GetResources().GetAxnodedMemoryUnboundedCount() != 1 {
		t.Fatalf("unexpected memory resources summary: %#v", summary.GetResources())
	}
	if summary.GetPools().GetCgroup().GetIdle() != 3 || summary.GetPools().GetInterface().GetIdle() != 4 {
		t.Fatalf("unexpected pools summary: %#v", summary.GetPools())
	}
	if summary.GetPools().GetInterface().GetUnavailable() != 2 {
		t.Fatalf("interface unavailable = %d, want 2", summary.GetPools().GetInterface().GetUnavailable())
	}
	if slots := summary.GetPools().GetRuntimeSlots(); slots.GetCapacity() != 8 || slots.GetUnavailable() != 2 || slots.GetUsing() != 2 {
		t.Fatalf("runtime slots = %+v, want capacity=8 unavailable=2 using=2", slots)
	}
	if !summary.GetComponents().GetAxnoded().GetReady() {
		t.Fatalf("axnoded ready = false, want true")
	}
	if got := summary.GetComponents().GetAxnoded().GetRunningAllocationIds(); len(got) != 2 || got[0] != "alloc-b" || got[1] != "alloc-a" {
		t.Fatalf("axnoded running allocation ids = %#v, want [alloc-b alloc-a]", got)
	}
	if got := summary.GetComponents().GetAxnoded().GetActiveAllocationIds(); len(got) != 3 || got[0] != "alloc-c" || got[1] != "alloc-b" || got[2] != "alloc-a" {
		t.Fatalf("axnoded active allocation ids = %#v, want [alloc-c alloc-b alloc-a]", got)
	}
	if summary.GetComponents().GetImagemgr().GetState().String() != "COMPONENT_STATE_DEGRADED" {
		t.Fatalf("unexpected imagemgr state: %v", summary.GetComponents().GetImagemgr().GetState())
	}
	if summary.GetComponents().GetImagemgr().GetMountedImageCount() != 3 || summary.GetComponents().GetImagemgr().GetImportedImageCount() != 4 {
		t.Fatalf("unexpected imagemgr image counts: %#v", summary.GetComponents().GetImagemgr())
	}
	if !summary.GetComponents().GetImagefsd().GetChunkdbPresent() || summary.GetComponents().GetImagefsd().GetChunkdbUsedBytes() != 2048 {
		t.Fatalf("unexpected imagefsd summary: %#v", summary.GetComponents().GetImagefsd())
	}
	if !summary.GetComponents().GetBpfnet().GetNeedsSnatFallback() || !summary.GetComponents().GetBpfnet().GetNeedsFullDnatFallback() {
		t.Fatalf("unexpected bpfnet summary: %#v", summary.GetComponents().GetBpfnet())
	}
	if summary.GetComponents().GetVolumed().GetState() != nodev1.ComponentState_COMPONENT_STATE_ERROR ||
		summary.GetComponents().GetVolumed().GetPublishedVolumeCount() != 2 ||
		summary.GetComponents().GetVolumed().GetLastReconcileError() != "provider validation failed" ||
		summary.GetComponents().GetVolumed().GetLastReconcileActiveAllocationCount() != 2 ||
		summary.GetComponents().GetVolumed().GetLastReconcileStaleAllocationCount() != 1 ||
		summary.GetComponents().GetVolumed().GetLastReconcileInvalidVolumeCount() != 1 ||
		summary.GetComponents().GetVolumed().GetLastReconcileAt().AsTime() != collectedAt.Add(time.Minute) {
		t.Fatalf("unexpected volumed summary: %#v", summary.GetComponents().GetVolumed())
	}
	if len(summary.GetStorage()) != 2 {
		t.Fatalf("storage len = %d, want 2", len(summary.GetStorage()))
	}
	if got := summary.GetStorage()[0]; got.GetTarget() != nodeinventory.StorageTargetAxnodedState ||
		got.GetCapacityBytes() != 1000 ||
		got.GetUsedBytes() != 250 ||
		got.GetAvailableBytes() != 700 ||
		got.GetInodesTotal() != 100 ||
		got.GetInodesUsed() != 10 ||
		got.GetInodesAvailable() != 90 ||
		!got.GetCollected() {
		t.Fatalf("unexpected collected storage summary: %#v", got)
	}
	if got := summary.GetStorage()[1]; got.GetTarget() != nodeinventory.StorageTargetImageCache ||
		got.GetCollected() ||
		got.GetError() != "statfs /var/lib/imagemgr: no such file or directory" {
		t.Fatalf("unexpected failed storage summary: %#v", got)
	}
	if len(summary.GetLocality()) != 1 {
		t.Fatalf("locality len = %d, want 1", len(summary.GetLocality()))
	}
	locality := summary.GetLocality()[0]
	if locality.GetKey() != "image:repo/app:latest" || !locality.GetMounted() {
		t.Fatalf("unexpected locality summary: %#v", locality)
	}
	if locality.GetRootfsType().String() != "ROOTFS_TYPE_IMAGE" || locality.GetMountType().String() != "MOUNT_TYPE_OCI" {
		t.Fatalf("unexpected locality types: %#v", locality)
	}
}
