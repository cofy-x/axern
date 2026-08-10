package placement

import (
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHasWarmRuntimeSlotUsesAggregateContract(t *testing.T) {
	pools := &nodev1.PoolsSummary{
		Cgroup:       &nodev1.PoolState{},
		Interface:    &nodev1.PoolState{Idle: 4},
		RuntimeSlots: &nodev1.PoolState{Idle: 4},
	}
	if !hasWarmRuntimeSlot(pools) {
		t.Fatal("hasWarmRuntimeSlot() = false, want aggregate idle slot to be authoritative")
	}
}

func TestPlanReturnsRejectedCandidatesWithExplicitReasons(t *testing.T) {
	engine := NewEngine(Config{
		HeartbeatFreshnessWindow: 15 * time.Second,
		SummaryFreshnessWindow:   15 * time.Second,
	})
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	snapshot := nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("stale-heartbeat", []string{"runsc"}, readySummary(base.Add(10*time.Second)), base),
			record("stale-summary", []string{"runsc"}, readySummary(base), base.Add(20*time.Second)),
			record("runtime-mismatch", []string{"runc"}, readySummary(base.Add(20*time.Second)), base.Add(20*time.Second)),
			record("eligible", []string{"runsc"}, readySummary(base.Add(20*time.Second)), base.Add(20*time.Second)),
		},
	}

	eligible, rejected := engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:  "local:/tmp/rootfs",
		RootfsType: nodev1.RootfsType_ROOTFS_TYPE_LOCAL,
		MountType:  nodev1.MountType_MOUNT_TYPE_LOCAL,
		Runtime:    "runsc",
	}, base.Add(20*time.Second))
	if len(eligible) != 1 || eligible[0].GetNodeID() != "eligible" {
		t.Fatalf("unexpected eligible candidates: %#v", eligible)
	}
	if len(rejected) != 3 {
		t.Fatalf("expected 3 rejected candidates, got %d", len(rejected))
	}
	assertRejectedReasons(t, rejected[0], "runtime-mismatch", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_RUNTIME_UNSUPPORTED)
	assertRejectedReasons(t, rejected[1], "stale-heartbeat", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_HEARTBEAT)
	assertRejectedReasons(t, rejected[2], "stale-summary",
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_SUMMARY,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED,
	)
}

func TestPlanComponentGatingByMountType(t *testing.T) {
	engine := NewEngine(Config{})
	now := time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC)

	localOnly := readySummary(now)
	localOnly.Components.Imagemgr.State = nodev1.ComponentState_COMPONENT_STATE_DISABLED
	localOnly.Components.Imagemgr.Reachable = false
	localOnly.Components.Imagefsd.State = nodev1.ComponentState_COMPONENT_STATE_DISABLED
	localOnly.Components.Imagefsd.Reachable = false

	snapshot := nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("local-node", []string{"runsc"}, localOnly, now),
			record("remote-node", []string{"runsc"}, readySummary(now), now),
		},
	}

	eligible, rejected := engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:  "image:repo/app:oci",
		RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:  nodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:    "runsc",
	}, now)
	if len(eligible) != 1 || eligible[0].GetNodeID() != "remote-node" {
		t.Fatalf("expected only remote node to be eligible for oci: %#v %#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "local-node", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE)

	eligible, rejected = engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:  "image:repo/app:nydus",
		RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:  nodev1.MountType_MOUNT_TYPE_NYDUS,
		Runtime:    "runsc",
	}, now)
	if len(eligible) != 1 || eligible[0].GetNodeID() != "remote-node" {
		t.Fatalf("expected only remote node to be eligible for nydus: %#v %#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "local-node",
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE,
	)
}

func TestEROFSLocalityRequiresObservedCompatibility(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	rootfsKey := "sha256:erofs-image"
	summary := readySummary(now)
	summary.Locality = []*nodev1.LocalitySummary{{Key: rootfsKey, RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE, MountType: nodev1.MountType_MOUNT_TYPE_EROFS, Mounted: true}}
	record := record("node-erofs", []string{"runsc"}, summary, now)
	request := &placementkernel.Request{RootfsKey: rootfsKey, RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE, MountType: nodev1.MountType_MOUNT_TYPE_OCI, Runtime: "runsc"}

	eligible, rejected := NewEngine(Config{}).Plan(nodekernel.Snapshot{Records: []*nodekernel.Record{record}}, request, now)
	if len(eligible) != 0 || len(rejected) != 1 {
		t.Fatalf("without EROFS evidence eligible=%#v rejected=%#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "node-erofs", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED)

	erofs := availableCapabilitySnapshot(now, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS).GetObservations()[0]
	summary.CapabilitySnapshot.Observations = append(summary.CapabilitySnapshot.Observations, erofs)
	eligible, rejected = NewEngine(Config{}).Plan(nodekernel.Snapshot{Records: []*nodekernel.Record{record}}, request, now)
	if len(eligible) != 1 || len(rejected) != 0 {
		t.Fatalf("with EROFS evidence eligible=%#v rejected=%#v", eligible, rejected)
	}
	candidateRequest, err := requestForCandidate(request, record, now)
	if err != nil {
		t.Fatal(err)
	}
	if candidateRequest.GetMountType() != nodev1.MountType_MOUNT_TYPE_EROFS || !containsPlatform(candidateRequest.GetCapabilityRequirements(), capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS) {
		t.Fatalf("candidate request = %#v, want EROFS dependency", candidateRequest)
	}
}

func TestPlanPrefersBetterBpfnetWhenPortsAreRequested(t *testing.T) {
	engine := NewEngine(Config{})
	now := time.Date(2026, 4, 21, 14, 0, 0, 0, time.UTC)

	baseline := readySummary(now)
	baseline.Locality = []*nodev1.LocalitySummary{{
		Key:                        "image:repo/app:latest",
		RootfsType:                 nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                  nodev1.MountType_MOUNT_TYPE_OCI,
		Mounted:                    true,
		RetainedRootfsCount:        1,
		RetainedRuntimeCount:       1,
		ChunkdbRecentAccessAgeSecs: 5,
		PeerHealthyCount:           2,
		PeerHintedCount:            1,
	}}
	needsFallback := proto.Clone(baseline).(*nodev1.NodeSummary)
	needsFallback.Components.Bpfnet.Ready = false
	needsFallback.Components.Bpfnet.NeedsFullDnatFallback = true

	snapshot := nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("node-fallback", []string{"runsc"}, needsFallback, now),
			record("node-preferred", []string{"runsc"}, baseline, now),
		},
	}

	eligible, _ := engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:  "image:repo/app:latest",
		RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:  nodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:    "runsc",
		Ports:      []string{"8080/tcp"},
	}, now)
	if len(eligible) != 2 || eligible[0].GetNodeID() != "node-preferred" {
		t.Fatalf("unexpected eligible candidates: %#v", eligible)
	}
	if !eligible[0].GetRank().GetBpfnetPreferred() {
		t.Fatalf("expected top-ranked node to have preferred bpfnet: %#v", eligible[0])
	}
}

func TestPlanSortsByFixedTuple(t *testing.T) {
	engine := NewEngine(Config{})
	now := time.Date(2026, 4, 21, 15, 0, 0, 0, time.UTC)

	hot := readySummary(now)
	hot.Resources.AxnodedUsedMilli = 300
	hot.Resources.AxnodedUsedBytes = 3000
	hot.Locality = []*nodev1.LocalitySummary{{
		Key:                        "image:repo/app:latest",
		RootfsType:                 nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                  nodev1.MountType_MOUNT_TYPE_OCI,
		Mounted:                    true,
		RetainedRootfsCount:        2,
		RetainedRuntimeCount:       1,
		ChunkdbRecentAccessAgeSecs: 5,
		PeerHealthyCount:           3,
		PeerHintedCount:            2,
	}}
	warm := readySummary(now)
	warm.Resources.AxnodedUsedMilli = 100
	warm.Resources.AxnodedUsedBytes = 1000
	warm.Locality = []*nodev1.LocalitySummary{{
		Key:                        "image:repo/app:latest",
		RootfsType:                 nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                  nodev1.MountType_MOUNT_TYPE_OCI,
		Mounted:                    false,
		RetainedRootfsCount:        1,
		RetainedRuntimeCount:       1,
		ChunkdbRecentAccessAgeSecs: 20,
		PeerHealthyCount:           1,
		PeerHintedCount:            1,
	}}

	eligible, _ := engine.Plan(nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("node-hot", []string{"runsc"}, hot, now),
			record("node-warm", []string{"runsc"}, warm, now),
		},
	}, &placementkernel.Request{
		RootfsKey:  "image:repo/app:latest",
		RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:  nodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:    "runsc",
	}, now)
	if len(eligible) != 2 || eligible[0].GetNodeID() != "node-hot" || eligible[1].GetNodeID() != "node-warm" {
		t.Fatalf("unexpected candidate order: %#v", eligible)
	}
	if eligible[0].GetState() != nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_ELIGIBLE {
		t.Fatalf("expected eligible state, got %#v", eligible[0])
	}
}

func TestPlanRejectsSelectorCapabilityAndResourceAdmission(t *testing.T) {
	engine := NewEngine(Config{})
	now := time.Date(2026, 4, 21, 16, 0, 0, 0, time.UTC)

	restricted := readySummary(now)
	restricted.Labels = map[string]string{"zone": "us-east-1"}
	restricted.CapabilitySnapshot = nil
	restricted.NodeState = nodev1.NodeState_NODE_STATE_DRAINING
	restricted.Allocatable = &commonv1.ResourceQuantity{
		CpuMilli:    1000,
		MemoryBytes: 1024,
	}
	restricted.Resources.AxnodedCommittedMilli = 900
	restricted.Resources.AxnodedCommittedBytes = 900

	eligible, rejected := engine.Plan(nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("node-a", []string{"runsc"}, restricted, now),
		},
	}, &placementkernel.Request{
		RootfsKey:                       "image:repo/app:latest",
		RootfsType:                      nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                       nodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:                         "runsc",
		RequestedCpuMilli:               200,
		RequestedMemoryBytes:            256,
		Ports:                           []string{"tcp:8080:80"},
		Network:                         "bridge",
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/gpu"}}},
		NodeSelector:                    map[string]string{"zone": "us-west-1"},
	}, now)
	if len(eligible) != 0 {
		t.Fatalf("expected no eligible candidates, got %#v", eligible)
	}
	assertRejectedReasons(t, rejected[0], "node-a",
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_DRAINING,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_SELECTOR_MISMATCH,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_PORTS_UNSUPPORTED,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED,
		nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED,
	)
}

func TestPlanRejectsInsufficientMemory(t *testing.T) {
	engine := NewEngine(Config{})
	now := time.Date(2026, 4, 21, 17, 0, 0, 0, time.UTC)

	summary := readySummary(now)
	summary.Allocatable = &commonv1.ResourceQuantity{
		CpuMilli:    4000,
		MemoryBytes: 2048,
	}
	summary.Resources.AxnodedCommittedBytes = 1800

	eligible, rejected := engine.Plan(nodekernel.Snapshot{
		Records: []*nodekernel.Record{
			record("node-mem", []string{"runsc"}, summary, now),
		},
	}, &placementkernel.Request{
		RootfsKey:            "image:repo/app:latest",
		RootfsType:           nodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:            nodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:              "runsc",
		RequestedMemoryBytes: 512,
	}, now)
	if len(eligible) != 0 {
		t.Fatalf("expected no eligible candidates, got %#v", eligible)
	}
	assertRejectedReasons(t, rejected[0], "node-mem", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY)
}

func TestPlanCPUOvercommitPolicy(t *testing.T) {
	engine := NewEngine(Config{ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2}})
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	summary := readySummary(now)
	summary.Allocatable = &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1 << 30}
	summary.Capacity = summary.Allocatable
	summary.Resources.AxnodedCommittedMilli = 1500

	snapshot := nodekernel.Snapshot{Records: []*nodekernel.Record{
		record("node-cpu", []string{"runsc"}, summary, now),
	}}
	eligible, rejected := engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:         "local:/tmp/rootfs",
		RootfsType:        nodev1.RootfsType_ROOTFS_TYPE_LOCAL,
		MountType:         nodev1.MountType_MOUNT_TYPE_LOCAL,
		Runtime:           "runsc",
		RequestedCpuMilli: 400,
	}, now)
	if len(eligible) != 1 || eligible[0].GetNodeID() != "node-cpu" || len(rejected) != 0 {
		t.Fatalf("expected node-cpu eligible, got eligible=%#v rejected=%#v", eligible, rejected)
	}

	eligible, rejected = engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:         "local:/tmp/rootfs",
		RootfsType:        nodev1.RootfsType_ROOTFS_TYPE_LOCAL,
		MountType:         nodev1.MountType_MOUNT_TYPE_LOCAL,
		Runtime:           "runsc",
		RequestedCpuMilli: 600,
	}, now)
	if len(eligible) != 0 || len(rejected) != 1 {
		t.Fatalf("expected node-cpu rejected, got eligible=%#v rejected=%#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "node-cpu", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU)
}

func TestPlanMemoryDoesNotOvercommit(t *testing.T) {
	engine := NewEngine(Config{ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2}})
	now := time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC)
	summary := readySummary(now)
	summary.Allocatable = &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1 << 30}
	summary.Capacity = summary.Allocatable
	summary.Resources.AxnodedCommittedBytes = 900 << 20

	snapshot := nodekernel.Snapshot{Records: []*nodekernel.Record{
		record("node-mem-overcommit", []string{"runsc"}, summary, now),
	}}
	eligible, rejected := engine.Plan(snapshot, &placementkernel.Request{
		RootfsKey:            "local:/tmp/rootfs",
		RootfsType:           nodev1.RootfsType_ROOTFS_TYPE_LOCAL,
		MountType:            nodev1.MountType_MOUNT_TYPE_LOCAL,
		Runtime:              "runsc",
		RequestedMemoryBytes: 200 << 20,
	}, now)
	if len(eligible) != 0 || len(rejected) != 1 {
		t.Fatalf("expected node rejected for memory, got eligible=%#v rejected=%#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "node-mem-overcommit", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY)
}

func TestPlanRejectsRetiredNodeAsNonRetryable(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	retired := record("node-retired", []string{"runsc"}, readySummary(now), now)
	retired.Lifecycle = nodekernel.LifecycleRetired
	eligible, rejected := NewEngine(Config{}).Plan(nodekernel.Snapshot{Records: []*nodekernel.Record{retired}}, &placementkernel.Request{Runtime: "runsc", MountType: nodev1.MountType_MOUNT_TYPE_LOCAL}, now)
	if len(eligible) != 0 || len(rejected) != 1 {
		t.Fatalf("eligible=%#v rejected=%#v", eligible, rejected)
	}
	assertRejectedReasons(t, rejected[0], "node-retired", nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_RETIRED)
}

func record(nodeID string, runtimes []string, summary *nodev1.NodeSummary, updatedAt time.Time) *nodekernel.Record {
	return &nodekernel.Record{
		NodeID:       nodeID,
		Lifecycle:    nodekernel.LifecycleActive,
		Runtimes:     append([]string(nil), runtimes...),
		Summary:      summary,
		RegisteredAt: updatedAt,
		UpdatedAt:    updatedAt,
	}
}

func assertRejectedReasons(t *testing.T, candidate *nodev1.PlacementCandidate, nodeID string, want ...nodev1.PlacementRejectionReason) {
	t.Helper()
	if candidate.GetNodeID() != nodeID {
		t.Fatalf("candidate node_id = %q, want %q", candidate.GetNodeID(), nodeID)
	}
	if candidate.GetState() != nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_REJECTED {
		t.Fatalf("candidate state = %v, want rejected", candidate.GetState())
	}
	got := candidate.GetRejectionReasons()
	if len(got) != len(want) {
		t.Fatalf("candidate %q rejection reasons = %#v, want %#v", nodeID, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %q rejection reasons = %#v, want %#v", nodeID, got, want)
		}
	}
}

func readySummary(collectedAt time.Time) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(collectedAt),
		NodeState:   nodev1.NodeState_NODE_STATE_READY,
		CapabilitySnapshot: availableCapabilitySnapshot(collectedAt,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
		),
		Resources: &nodev1.ResourcesSummary{
			AxnodedUsedMilli: 100,
			AxnodedUsedBytes: 1000,
		},
		Allocatable: &commonv1.ResourceQuantity{
			CpuMilli:    8000,
			MemoryBytes: 16 << 30,
		},
		Capacity: &commonv1.ResourceQuantity{
			CpuMilli:    8000,
			MemoryBytes: 16 << 30,
		},
		Pools: &nodev1.PoolsSummary{
			RuntimeSlots: &nodev1.PoolState{Idle: 8, Capacity: 8},
			Cgroup:       &nodev1.PoolState{Idle: 1, Capacity: 8},
			Interface:    &nodev1.PoolState{Idle: 1, Capacity: 8},
		},
		Components: &nodev1.ComponentsSummary{
			Axnoded: &nodev1.AxnodedSummary{
				State: nodev1.ComponentState_COMPONENT_STATE_READY,
				Ready: true,
			},
			Imagemgr: &nodev1.ImagemgrSummary{
				State:     nodev1.ComponentState_COMPONENT_STATE_READY,
				Reachable: true,
			},
			Imagefsd: &nodev1.ImagefsdSummary{
				State:     nodev1.ComponentState_COMPONENT_STATE_READY,
				Reachable: true,
			},
			Bpfnet: &nodev1.BpfNetSummary{
				State:                 nodev1.ComponentState_COMPONENT_STATE_READY,
				Enabled:               true,
				Ready:                 true,
				NeedsSnatFallback:     false,
				NeedsFullDnatFallback: false,
				NeedsLocalhostCompat:  false,
			},
		},
	}
}

func availableCapabilitySnapshot(observedAt time.Time, platforms ...capabilityv1.PlatformCapability) *capabilityv1.CapabilitySnapshot {
	return controldtest.AvailableCapabilitySnapshot(observedAt, platforms...)
}
