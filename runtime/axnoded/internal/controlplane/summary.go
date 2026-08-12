package controlplane

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BuildNodeSummary(snapshot nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary {
	summary := &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(snapshot.Node.CollectedAt),
		Resources: &nodev1.ResourcesSummary{
			AxnodedCommittedMilli:                 snapshot.Resources.CPU.AxnodedCommittedMilli,
			AxnodedUsedMilli:                      snapshot.Resources.CPU.AxnodedUsedMilli,
			AxnodedCpuUnboundedCount:              snapshot.Resources.CPU.AxnodedUnboundedCount,
			AxnodedCommittedBytes:                 snapshot.Resources.Memory.AxnodedCommittedBytes,
			AxnodedUsedBytes:                      snapshot.Resources.Memory.AxnodedUsedBytes,
			AxnodedMemoryUnboundedCount:           snapshot.Resources.Memory.AxnodedUnboundedCount,
			AxnodedEphemeralStorageCommittedBytes: snapshot.Resources.EphemeralStorage.AxnodedCommittedBytes,
			AxnodedEphemeralStorageUsedBytes:      snapshot.Resources.EphemeralStorage.AxnodedUsedBytes,
			AxnodedEphemeralStorageUnboundedCount: snapshot.Resources.EphemeralStorage.AxnodedUnboundedCount,
		},
		Pools: &nodev1.PoolsSummary{
			RuntimeSlots: &nodev1.PoolState{
				Using:       int32(snapshot.Pools.RuntimeSlots.Using),
				Idle:        int32(snapshot.Pools.RuntimeSlots.Idle),
				Capacity:    int32(snapshot.Pools.RuntimeSlots.Capacity),
				Unavailable: int32(snapshot.Pools.RuntimeSlots.Unavailable),
			},
			Cgroup: &nodev1.PoolState{
				Using:       int32(snapshot.Pools.Cgroup.Using),
				Idle:        int32(snapshot.Pools.Cgroup.Idle),
				Capacity:    int32(snapshot.Pools.Cgroup.Capacity),
				Unavailable: int32(snapshot.Pools.Cgroup.Unavailable),
			},
			Interface: &nodev1.PoolState{
				Using:       int32(snapshot.Pools.Interface.Using),
				Idle:        int32(snapshot.Pools.Interface.Idle),
				Capacity:    int32(snapshot.Pools.Interface.Capacity),
				Unavailable: int32(snapshot.Pools.Interface.Unavailable),
			},
		},
		Components: &nodev1.ComponentsSummary{
			Axnoded: &nodev1.AxnodedSummary{
				State:                componentStateFromString(snapshot.Components.Axnoded.Status),
				Ready:                snapshot.Components.Axnoded.Ready,
				RunningContainers:    int32(snapshot.Components.Axnoded.RunningContainers),
				RunningAllocationIds: append([]string(nil), snapshot.Components.Axnoded.RunningAllocationIDs...),
				ActiveAllocationIds:  append([]string(nil), snapshot.Components.Axnoded.ActiveAllocationIDs...),
			},
			Imagemgr: &nodev1.ImagemgrSummary{
				State:              componentStateFromString(snapshot.Components.Imagemgr.Status),
				Reachable:          snapshot.Components.Imagemgr.Reachable,
				DaemonCount:        int32(snapshot.Components.Imagemgr.DaemonCount),
				MountedImageCount:  int32(snapshot.Components.Imagemgr.MountedImageCount),
				ImportedImageCount: int32(snapshot.Components.Imagemgr.ImportedImageCount),
			},
			Imagefsd: &nodev1.ImagefsdSummary{
				State:               componentStateFromString(snapshot.Components.Imagefsd.Status),
				Reachable:           snapshot.Components.Imagefsd.Reachable,
				ChunkdbPresent:      snapshot.Components.Imagefsd.ChunkDBPresent,
				ChunkCount:          snapshot.Components.Imagefsd.ChunkCount,
				ChunkdbUsedBytes:    snapshot.Components.Imagefsd.ChunkDBUsedBytes,
				ChunkdbUsagePercent: snapshot.Components.Imagefsd.ChunkDBUsagePercent,
			},
			Bpfnet: &nodev1.BpfNetSummary{
				State:                 componentStateFromString(snapshot.Components.BPFNet.Status),
				Enabled:               snapshot.Components.BPFNet.Enabled,
				Ready:                 snapshot.Components.BPFNet.Ready,
				Mode:                  snapshot.Components.BPFNet.Mode,
				NeedsSnatFallback:     snapshot.Components.BPFNet.NeedsSNATFallback,
				NeedsFullDnatFallback: snapshot.Components.BPFNet.NeedsFullDNATFallback,
				NeedsLocalhostCompat:  snapshot.Components.BPFNet.NeedsLocalhostCompat,
			},
			Volumed: &nodev1.VolumedSummary{
				State:                              componentStateFromString(snapshot.Components.Volumed.Status),
				Reachable:                          snapshot.Components.Volumed.Reachable,
				PublishedVolumeCount:               int32(snapshot.Components.Volumed.PublishedVolumeCount),
				LastReconcileError:                 snapshot.Components.Volumed.LastReconcileError,
				LastReconcileRetainedCount:         int32(snapshot.Components.Volumed.LastReconcileRetainedCount),
				LastReconcileUnpublishedCount:      int32(snapshot.Components.Volumed.LastReconcileUnpublishedCount),
				LastReconcileActiveAllocationCount: int32(snapshot.Components.Volumed.LastReconcileActiveAllocationCount),
				LastReconcileStaleAllocationCount:  int32(snapshot.Components.Volumed.LastReconcileStaleAllocationCount),
				LastReconcileInvalidVolumeCount:    int32(snapshot.Components.Volumed.LastReconcileInvalidVolumeCount),
			},
		},
		Locality:           make([]*nodev1.LocalitySummary, 0, len(snapshot.Heat.Locality)),
		NodeState:          nodeStateFromString(snapshot.Node.State),
		Labels:             cloneNodeLabels(snapshot.Node.Labels),
		CapabilitySnapshot: cloneCapabilitySnapshot(snapshot.Node.CapabilitySnapshot),
		Capacity: &commonv1.ResourceQuantity{
			CpuMilli: snapshot.Node.Capacity.CpuMilli, MemoryBytes: snapshot.Node.Capacity.MemoryBytes,
			EphemeralStorageBytes: snapshot.Node.Capacity.EphemeralStorageBytes,
		},
		Allocatable: &commonv1.ResourceQuantity{
			CpuMilli: snapshot.Node.Allocatable.CpuMilli, MemoryBytes: snapshot.Node.Allocatable.MemoryBytes,
			EphemeralStorageBytes: snapshot.Node.Allocatable.EphemeralStorageBytes,
		},
		Storage: make([]*nodev1.NodeStorageSummary, 0, len(snapshot.Storage)),
		MemoryBudget: &nodev1.NodeMemoryBudget{
			PhysicalCapacityBytes:     snapshot.Node.MemoryBudget.PhysicalCapacityBytes,
			SourceAllocatableBytes:    snapshot.Node.MemoryBudget.SourceAllocatableBytes,
			DelegatedRootLimitBytes:   snapshot.Node.MemoryBudget.DelegatedRootLimitBytes,
			DelegatedRootLimitFinite:  snapshot.Node.MemoryBudget.DelegatedRootLimitFinite,
			SystemReserveBytes:        snapshot.Node.MemoryBudget.SystemReserveBytes,
			EffectiveAllocatableBytes: snapshot.Node.MemoryBudget.EffectiveAllocatableBytes,
			LocalCommitmentBytes:      snapshot.Node.MemoryBudget.LocalCommitmentBytes,
			CleanupDebtBytes:          snapshot.Node.MemoryBudget.CleanupDebtBytes,
			InternalCurrentBytes:      snapshot.Node.MemoryBudget.InternalCurrentBytes,
			CapacityIdentity:          snapshot.Node.MemoryBudget.CapacityIdentity,
			Mode:                      memoryBudgetModeToProto(snapshot.Node.MemoryBudget.Mode),
			RetiringCgroupCount:       int32(snapshot.Node.MemoryBudget.RetiringCgroupCount),
			OldestRetiringAgeSeconds:  snapshot.Node.MemoryBudget.OldestRetiringAgeSeconds,
			SystemReserveExhausted:    snapshot.Node.MemoryBudget.SystemReserveExhausted,
		},
	}
	if !snapshot.Node.MemoryBudget.SampledAt.IsZero() {
		summary.MemoryBudget.SampledAt = timestamppb.New(snapshot.Node.MemoryBudget.SampledAt)
	}
	if !snapshot.Components.Volumed.LastReconcileAt.IsZero() {
		summary.Components.Volumed.LastReconcileAt = timestamppb.New(snapshot.Components.Volumed.LastReconcileAt)
	}
	for _, entry := range snapshot.Heat.Locality {
		summary.Locality = append(summary.Locality, &nodev1.LocalitySummary{
			Key:                        entry.Key,
			RootfsType:                 rootfsTypeToProto(entry.RootfsType),
			MountType:                  mountTypeToProto(entry.MountType),
			Mounted:                    entry.Mounted,
			RetainedRuntimeCount:       int32(entry.RetainedRuntimeCount),
			RetainedRootfsCount:        int32(entry.RetainedRootfsCount),
			RunningContainerCount:      int32(entry.RunningContainerCount),
			NydusDaemonAlive:           entry.NydusDaemonAlive,
			ChunkdbTotalChunks:         entry.ChunkDBTotalChunks,
			ChunkdbUsedBytes:           entry.ChunkDBUsedBytes,
			ChunkdbRecentAccessAgeSecs: entry.ChunkDBRecentAccessAgeSecs,
			PeerHealthyCount:           entry.PeerHealthyCount,
			PeerUnhealthyCount:         entry.PeerUnhealthyCount,
			PeerHintedCount:            entry.PeerHintedCount,
		})
	}
	for _, entry := range snapshot.Storage {
		summary.Storage = append(summary.Storage, &nodev1.NodeStorageSummary{
			Target:                      entry.Target,
			CapacityBytes:               entry.CapacityBytes,
			UsedBytes:                   entry.UsedBytes,
			AvailableBytes:              entry.AvailableBytes,
			InodesTotal:                 entry.InodesTotal,
			InodesUsed:                  entry.InodesUsed,
			InodesAvailable:             entry.InodesAvailable,
			Collected:                   entry.Collected,
			Error:                       entry.Error,
			SystemReserveBytes:          entry.SystemReserveBytes,
			ReservedBytes:               entry.ReservedBytes,
			AllocatableBytes:            entry.AllocatableBytes,
			ActiveReservations:          entry.ActiveReservations,
			FilesystemType:              entry.FilesystemType,
			MountIdentity:               entry.MountIdentity,
			AllocationUsedBytes:         entry.AllocationUsedBytes,
			UnlinkedBackingUsageUnknown: entry.UnlinkedBackingUsageUnknown,
		})
	}
	return summary
}

func memoryBudgetModeToProto(mode string) nodev1.NodeMemoryBudgetMode {
	switch mode {
	case "cgroup_v2":
		return nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_CGROUP_V2
	case "disabled_dev":
		return nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_DISABLED_DEV
	default:
		return nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_UNSPECIFIED
	}
}

func cloneCapabilitySnapshot(in *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilitySnapshot)
}

func nodeStateFromString(state string) nodev1.NodeState {
	switch state {
	case "ready":
		return nodev1.NodeState_NODE_STATE_READY
	case "draining":
		return nodev1.NodeState_NODE_STATE_DRAINING
	case "disabled":
		return nodev1.NodeState_NODE_STATE_DISABLED
	default:
		return nodev1.NodeState_NODE_STATE_UNSPECIFIED
	}
}

func cloneNodeLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func componentStateFromString(status string) nodev1.ComponentState {
	switch status {
	case nodeinventory.StatusReady:
		return nodev1.ComponentState_COMPONENT_STATE_READY
	case nodeinventory.StatusWarming:
		return nodev1.ComponentState_COMPONENT_STATE_WARMING
	case nodeinventory.StatusDegraded:
		return nodev1.ComponentState_COMPONENT_STATE_DEGRADED
	case nodeinventory.StatusError:
		return nodev1.ComponentState_COMPONENT_STATE_ERROR
	case nodeinventory.StatusDisabled:
		return nodev1.ComponentState_COMPONENT_STATE_DISABLED
	default:
		return nodev1.ComponentState_COMPONENT_STATE_UNSPECIFIED
	}
}

func rootfsTypeToProto(rootfsType string) nodev1.RootfsType {
	switch rootfsType {
	case "local":
		return nodev1.RootfsType_ROOTFS_TYPE_LOCAL
	case "image":
		return nodev1.RootfsType_ROOTFS_TYPE_IMAGE
	case "s3":
		return nodev1.RootfsType_ROOTFS_TYPE_S3
	default:
		return nodev1.RootfsType_ROOTFS_TYPE_UNSPECIFIED
	}
}

func mountTypeToProto(mountType string) nodev1.MountType {
	switch mountType {
	case "local":
		return nodev1.MountType_MOUNT_TYPE_LOCAL
	case "oci":
		return nodev1.MountType_MOUNT_TYPE_OCI
	case "nydus":
		return nodev1.MountType_MOUNT_TYPE_NYDUS
	case "oss":
		return nodev1.MountType_MOUNT_TYPE_OSS
	case "erofs":
		return nodev1.MountType_MOUNT_TYPE_EROFS
	default:
		return nodev1.MountType_MOUNT_TYPE_UNSPECIFIED
	}
}
