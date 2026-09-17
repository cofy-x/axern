package placement

import (
	"slices"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
)

func dedupeRejectionReasons(in []placementkernel.RejectionReason) []placementkernel.RejectionReason {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[placementkernel.RejectionReason]struct{}, len(in))
	out := make([]placementkernel.RejectionReason, 0, len(in))
	for _, reason := range in {
		if reason == placementkernel.RejectionReasonUnspecified {
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

func buildPlacementRank(req *placementkernel.Request, summary *nodev1.NodeSummary, locality *nodev1.LocalitySummary) *placementkernel.Rank {
	rank := &placementkernel.Rank{
		MountedMatch:               localityMounted(locality, req.GetRootfsKey()),
		RetainedRootfsCount:        locality.GetRetainedRootfsCount(),
		RetainedEnvironmentCount:   locality.GetRetainedEnvironmentCount(),
		NydusDaemonAlive:           locality.GetNydusDaemonAlive(),
		ChunkDBRecentAccessAgeSecs: localityAge(locality),
		PeerHealthyCount:           locality.GetPeerHealthyCount(),
		PeerHintedCount:            locality.GetPeerHintedCount(),
		IdlePoolReady:              hasWarmRuntimeSlot(summary.GetPools()),
		RuntimeSlotOccupancy:       nodekernel.ReportedActiveInstances(summary),
	}
	return rank
}

func compareEligibleCandidates(left, right *placementkernel.Evaluation) bool {
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
