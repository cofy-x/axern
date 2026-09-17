package placement

import (
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/memorybudget"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

type CandidateInput struct {
	Record  *nodekernel.Record
	Request *placementkernel.Request
	Now     time.Time
}

func (e *Engine) evaluateCandidate(input CandidateInput) *placementkernel.Evaluation {
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
	candidate := &placementkernel.Evaluation{
		NodeID:           record.NodeID,
		State:            placementkernel.CandidateStateEligible,
		HeartbeatAgeSecs: nodekernel.HeartbeatAgeSecs(record.LastHeartbeatAt, input.Now),
		SummaryAgeSecs:   nodekernel.SummaryAgeSecs(summary, input.Now),
		Pools:            clonePools(summary.GetPools()),
		Locality:         locality,
		Rank:             buildPlacementRank(input.Request, summary, locality),
	}

	reasons := make([]placementkernel.RejectionReason, 0, 4)
	if !record.Active() {
		reasons = append(reasons, placementkernel.RejectionReasonNodeRetired)
	}
	heartbeatFresh := nodekernel.HeartbeatFresh(record.LastHeartbeatAt, input.Now, e.heartbeatFreshnessWindow)
	summaryFresh := nodekernel.SummaryFresh(summary, input.Now, e.summaryFreshnessWindow)
	if !heartbeatFresh {
		reasons = append(reasons, placementkernel.RejectionReasonStaleHeartbeat)
	}
	if !summaryFresh {
		reasons = append(reasons, placementkernel.RejectionReasonStaleSummary)
	}
	switch effectiveNodeState(summary, heartbeatFresh) {
	case nodev1.NodeState_NODE_STATE_DRAINING:
		reasons = append(reasons, placementkernel.RejectionReasonNodeDraining)
	case nodev1.NodeState_NODE_STATE_DISABLED:
		reasons = append(reasons, placementkernel.RejectionReasonNodeDisabled)
	}
	if !labelsMatch(summary.GetLabels(), input.Request.GetNodeSelector()) {
		reasons = append(reasons, placementkernel.RejectionReasonNodeSelectorMismatch)
	}
	if summary == nil || !summary.GetComponents().GetAxnoded().GetReady() {
		reasons = append(reasons, placementkernel.RejectionReasonAxnodedNotReady)
	}
	if summary.GetMemoryBudget().GetSystemReserveExhausted() {
		reasons = append(reasons, placementkernel.RejectionReasonNodeMemorySystemReserveExhausted)
	}
	// Every sandbox consumes the delegated node memory domain even when the
	// workload declares a zero request. Missing or stale capacity evidence must
	// therefore reject all placement instead of allowing an unreserved workload
	// to bypass the node-local admission boundary.
	if err := memorybudget.ValidateSummary(summary, input.Now); err != nil {
		reasons = append(reasons, placementkernel.RejectionReasonNodeMemoryBudgetUnavailable)
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
			reasons = append(reasons, placementkernel.RejectionReasonImagemgrUnavailable)
		}
	case nodev1.MountType_MOUNT_TYPE_NYDUS:
		if !imagemgrReady {
			reasons = append(reasons, placementkernel.RejectionReasonImagemgrUnavailable)
		}
		if !imagefsdReady {
			reasons = append(reasons, placementkernel.RejectionReasonImagefsdUnavailable)
		}
	default:
		reasons = append(reasons,
			placementkernel.RejectionReasonImagemgrUnavailable,
			placementkernel.RejectionReasonImagefsdUnavailable,
		)
	}
	if derivationErr != nil && requiresNodeDataplane(input.Request) {
		reasons = append(reasons, placementkernel.RejectionReasonNetworkUnsupported)
	}
	reasons = append(reasons, missingCapabilityRejectionReasons(summary, input.Request.GetCapabilityRequirements(), input.Now)...)
	if !hasAvailableCPU(e.resourcePolicy, summary, input.Request.GetRequestedCpuMilli()) {
		reasons = append(reasons, placementkernel.RejectionReasonInsufficientCPU)
	}
	if !hasAvailableMemory(e.resourcePolicy, summary, input.Request.GetRequestedMemoryBytes()) {
		reasons = append(reasons, placementkernel.RejectionReasonInsufficientMemory)
	}
	if !hasAvailableEphemeralStorage(e.resourcePolicy, summary, input.Request.GetRequestedEphemeralStorageBytes()) {
		reasons = append(reasons, placementkernel.RejectionReasonInsufficientEphemeralStorage)
	}

	if len(reasons) > 0 {
		candidate.State = placementkernel.CandidateStateRejected
		candidate.RejectionReasons = dedupeRejectionReasons(reasons)
	}
	return candidate
}

func missingCapabilityRejectionReasons(summary *nodev1.NodeSummary, requirements []*capabilityv1.CapabilityKey, now time.Time) []placementkernel.RejectionReason {
	reasons := make([]placementkernel.RejectionReason, 0, 2)
	for _, requirement := range requirements {
		if hasCapability(summary, requirement, now) {
			continue
		}
		switch requirement.GetPlatform() {
		case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET:
			reasons = append(reasons, placementkernel.RejectionReasonNetworkUnsupported)
		default:
			reasons = append(reasons, placementkernel.RejectionReasonCapabilityUnsupported)
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

func cloneLocality(in *nodev1.LocalitySummary) *nodev1.LocalitySummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.LocalitySummary)
}
