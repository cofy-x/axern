package placement

import (
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

type CandidateInput struct {
	Record  *nodekernel.Record
	Request *Request
	Now     time.Time
}

func (e *Engine) evaluateCandidate(input CandidateInput) *nodev1.PlacementCandidate {
	record := input.Record
	if record == nil {
		return nil
	}

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
	switch input.Request.GetMountType() {
	case nodev1.MountType_MOUNT_TYPE_LOCAL:
	case nodev1.MountType_MOUNT_TYPE_OCI:
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
	if requiresPortsCapability(input.Request) && !hasCapability(summary, "feature:ports") {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_PORTS_UNSUPPORTED)
	}
	if network := requiredNetworkCapability(input.Request); network != "" && !hasCapability(summary, network) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED)
	}
	if !hasCapabilities(summary, input.Request.GetCapabilityRequirements()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED)
	}
	if !hasAvailableCPU(e.resourcePolicy, summary, input.Request.GetRequestedCpuMilli()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU)
	}
	if !hasAvailableMemory(e.resourcePolicy, summary, input.Request.GetRequestedMemoryBytes()) {
		reasons = append(reasons, nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY)
	}

	if len(reasons) > 0 {
		candidate.State = nodev1.PlacementCandidateState_PLACEMENT_CANDIDATE_STATE_REJECTED
		candidate.RejectionReasons = dedupeRejectionReasons(reasons)
	}
	return candidate
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
