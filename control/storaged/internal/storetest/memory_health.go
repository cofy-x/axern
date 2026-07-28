package storetest

import (
	"context"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
)

func (s *MemoryStore) GetVolumeBindingHealth(_ context.Context, releasingStuckAfter time.Duration) (*privatestoragev1.VolumeBindingHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	bindingCounts := map[storagev1.VolumeStatus]int64{}
	claimCounts := map[storagev1.VolumeStatus]int64{}
	var stuckReleasingBindings int64
	cutoff := time.Now().UTC().Add(-releasingStuckAfter)
	for _, binding := range s.bindings {
		if binding == nil {
			continue
		}
		bindingCounts[binding.GetStatus()]++
		if releasingStuckAfter > 0 && binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_RELEASING && binding.GetUpdatedAt().IsValid() && !binding.GetUpdatedAt().AsTime().After(cutoff) {
			stuckReleasingBindings++
		}
	}
	for _, claim := range s.claims {
		if claim != nil {
			claimCounts[claim.GetStatus()]++
		}
	}
	claims := make([]kernel.ClaimHealthState, 0, len(s.claims))
	for _, claim := range s.claims {
		if claim == nil {
			continue
		}
		claims = append(claims, kernel.ClaimHealthState{ClaimID: claim.GetID(), Status: claim.GetStatus()})
	}
	bindings := make([]*privatestoragev1.VolumeBinding, 0, len(s.bindings))
	for _, binding := range s.bindings {
		bindings = append(bindings, binding)
	}
	health := kernel.BuildVolumeBindingHealth(bindingCounts, claimCounts, stuckReleasingBindings, kernel.EvaluateHealthConsistency(claims, bindings))
	return proto.Clone(health).(*privatestoragev1.VolumeBindingHealth), nil
}
