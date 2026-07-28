package placement

import (
	"slices"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func dedupeRejectionReasons(in []nodev1.PlacementRejectionReason) []nodev1.PlacementRejectionReason {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[nodev1.PlacementRejectionReason]struct{}, len(in))
	out := make([]nodev1.PlacementRejectionReason, 0, len(in))
	for _, reason := range in {
		if reason == nodev1.PlacementRejectionReason_PLACEMENT_REJECTION_REASON_UNSPECIFIED {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	slices.Sort(out)
	return out
}

func buildPlacementRank(req *Request, summary *nodev1.NodeSummary, locality *nodev1.LocalitySummary) *nodev1.PlacementRank {
	rank := &nodev1.PlacementRank{
		MountedMatch:               localityMounted(locality, req.GetRootfsKey()),
		RetainedRootfsCount:        locality.GetRetainedRootfsCount(),
		RetainedRuntimeCount:       locality.GetRetainedRuntimeCount(),
		NydusDaemonAlive:           locality.GetNydusDaemonAlive(),
		ChunkdbRecentAccessAgeSecs: localityAge(locality),
		PeerHealthyCount:           locality.GetPeerHealthyCount(),
		PeerHintedCount:            locality.GetPeerHintedCount(),
		IdlePoolReady:              hasWarmRuntimeSlot(summary.GetPools()),
		AxnodedUsedMilli:           summary.GetResources().GetAxnodedUsedMilli(),
		AxnodedUsedBytes:           summary.GetResources().GetAxnodedUsedBytes(),
		AxnodedActiveInstances:     nodekernel.ReportedActiveInstances(summary),
	}
	if requiresPortsCapability(req) {
		rank.BpfnetPreferred = bpfnetPreferred(summary)
	}
	return rank
}

func compareEligibleCandidates(left, right *nodev1.PlacementCandidate) bool {
	return placementkernel.EvaluationLess(left, right)
}

func findMatchingLocality(entries []*nodev1.LocalitySummary, key string) *nodev1.LocalitySummary {
	for _, entry := range entries {
		if entry != nil && entry.GetKey() == key {
			return entry
		}
	}
	return nil
}

func localityMounted(locality *nodev1.LocalitySummary, key string) bool {
	return locality != nil && locality.GetKey() == key && locality.GetMounted()
}

func localityAge(locality *nodev1.LocalitySummary) int64 {
	if locality == nil {
		return 1<<62 - 1
	}
	age := locality.GetChunkdbRecentAccessAgeSecs()
	if age <= 0 {
		return 1<<62 - 1
	}
	return age
}

func hasWarmRuntimeSlot(pools *nodev1.PoolsSummary) bool {
	if pools == nil {
		return false
	}
	return pools.GetRuntimeSlots().GetIdle() > 0
}

func bpfnetPreferred(summary *nodev1.NodeSummary) bool {
	if summary == nil {
		return false
	}
	component := summary.GetComponents().GetBpfnet()
	return component.GetReady() && !component.GetNeedsFullDnatFallback()
}
