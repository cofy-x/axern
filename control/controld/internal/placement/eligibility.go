package placement

import (
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

type CandidateInput struct {
	Record  *nodekernel.Record
	Request *placementkernel.Request
	Now     time.Time
}

func (e *Engine) evaluateCandidate(input CandidateInput) *nodev1.PlacementCandidate {
	record := input.Record
	if record == nil {
		return nil
	}
	request, derivationErr := placementkernel.ResolveRequestForNode(input.Request, record.Summary, input.Now)
	if request == nil {
		return nil
	}
	input.Request = request

	summary := record.Summary
	locality := cloneLocality(findMatchingLocality(summary.GetLocality(), input.Request.GetRootfsKey()))
	candidate := &nodev1.PlacementCandidate{
		NodeID:           record.NodeID,
		State:            nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_ELIGIBLE,
		HeartbeatAgeSecs: nodekernel.HeartbeatAgeSecs(record.UpdatedAt, input.Now),
		SummaryAgeSecs:   nodekernel.SummaryAgeSecs(summary, input.Now),
		Pools:            clonePools(summary.GetPools()),
		Resources:        cloneResources(summary.GetResources()),
		Locality:         locality,
		Rank:             buildPlacementRank(input.Request, summary, locality),
	}

	reasons := make([]nodev1.PlacementRejectionReason, 0, 4)
	if !record.Active() {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_RETIRED)
	}
	heartbeatFresh := nodekernel.HeartbeatFresh(record.UpdatedAt, input.Now, e.heartbeatFreshnessWindow)
	summaryFresh := nodekernel.SummaryFresh(summary, input.Now, e.summaryFreshnessWindow)
	if !heartbeatFresh {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_HEARTBEAT)
	}
	if !summaryFresh {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_STALE_SUMMARY)
	}
	if !containsRuntime(record.Runtimes, input.Request.GetRuntime()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_RUNTIME_UNSUPPORTED)
	}
	switch effectiveNodeState(summary, heartbeatFresh) {
	case nodev1.NodeState_NODE_STATE_DRAINING:
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_DRAINING)
	case nodev1.NodeState_NODE_STATE_DISABLED:
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_DISABLED)
	}
	if !labelsMatch(summary.GetLabels(), input.Request.GetNodeSelector()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NODE_SELECTOR_MISMATCH)
	}
	if summary == nil || !summary.GetComponents().GetAxnoded().GetReady() {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_AXNODED_NOT_READY)
	}

	imagemgrReady := imagemgrUsable(summary)
	imagefsdReady := imagefsdUsable(summary)
	mountType := input.Request.GetMountType()
	if locality.GetMountType() == nodev1.MountType_MOUNT_TYPE_EROFS {
		mountType = nodev1.MountType_MOUNT_TYPE_EROFS
	}
	switch mountType {
	case nodev1.MountType_MOUNT_TYPE_LOCAL:
	case nodev1.MountType_MOUNT_TYPE_OCI, nodev1.MountType_MOUNT_TYPE_EROFS:
		if !imagemgrReady {
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE)
		}
	case nodev1.MountType_MOUNT_TYPE_NYDUS, nodev1.MountType_MOUNT_TYPE_OSS:
		if !imagemgrReady {
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE)
		}
		if !imagefsdReady {
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE)
		}
	default:
		reasons = append(reasons,
			nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE,
			nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE,
		)
	}
	if derivationErr != nil && requiresNodeDataplane(input.Request) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED)
	}
	reasons = append(reasons, missingCapabilityRejectionReasons(summary, input.Request.GetCapabilityRequirements(), input.Now)...)
	if !hasAvailableCPU(e.resourcePolicy, summary, input.Request.GetRequestedCpuMilli()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU)
	}
	if !hasAvailableMemory(e.resourcePolicy, summary, input.Request.GetRequestedMemoryBytes()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY)
	}
	if !hasAvailableEphemeralStorage(e.resourcePolicy, summary, input.Request.GetRequestedEphemeralStorageBytes()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_EPHEMERAL_STORAGE)
	}

	if len(reasons) > 0 {
		candidate.State = nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_REJECTED
		candidate.RejectionReasons = dedupeRejectionReasons(reasons)
	}
	return candidate
}

func missingCapabilityRejectionReasons(summary *nodev1.NodeSummary, requirements []*capabilityv1.CapabilityKey, now time.Time) []nodev1.PlacementRejectionReason {
	reasons := make([]nodev1.PlacementRejectionReason, 0, 2)
	for _, requirement := range requirements {
		if hasCapability(summary, requirement, now) {
			continue
		}
		switch requirement.GetPlatform() {
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING:
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_PORTS_UNSUPPORTED)
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET:
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED)
		default:
			reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED)
		}
	}
	return dedupeRejectionReasons(reasons)
}

func clonePools(in *nodev1.PoolsSummary) *nodev1.PoolsSummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.PoolsSummary)
}

func cloneResources(in *nodev1.ResourcesSummary) *nodev1.ResourcesSummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.ResourcesSummary)
}

func cloneLocality(in *nodev1.LocalitySummary) *nodev1.LocalitySummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.LocalitySummary)
}
