package storetest

import (
	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
)

func cloneRuntimeCompatibility(in *storagev1.VolumeRuntimeCompatibility) *storagev1.VolumeRuntimeCompatibility {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*storagev1.VolumeRuntimeCompatibility)
}

func cloneTopology(in *storagev1.VolumeTopology) *storagev1.VolumeTopology {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*storagev1.VolumeTopology)
}

func (s *MemoryStore) updateClaimFromBindingLocked(binding *privatestoragev1.VolumeBinding) error {
	for _, claim := range s.claims {
		if claim.GetID() != binding.GetClaimID() {
			continue
		}
		if err := kernel.TransitionClaimStatus(claim.GetStatus(), binding.GetStatus()); err != nil {
			return err
		}
		claim.Status = binding.GetStatus()
		claim.Topology = cloneTopology(binding.GetResolvedVolume().GetTopology())
		claim.Message = binding.GetMessage()
		claim.Version++
	}
	return nil
}

func (s *MemoryStore) recomputeClaimStatusLocked(claimID string) error {
	var claim *storagev1.VolumeClaim
	for _, item := range s.claims {
		if item.GetID() == claimID {
			claim = item
			break
		}
	}
	if claim == nil || claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil
	}
	nextStatus := storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	message := ""
	for _, binding := range s.bindings {
		if binding.GetClaimID() != claimID || binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			continue
		}
		switch binding.GetStatus() {
		case storagev1.VolumeStatus_VOLUME_STATUS_FAILED:
			nextStatus = storagev1.VolumeStatus_VOLUME_STATUS_FAILED
			message = binding.GetMessage()
		case storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			if nextStatus != storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
				nextStatus = storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED
			}
		}
	}
	if err := kernel.TransitionClaimStatus(claim.GetStatus(), nextStatus); err != nil {
		return err
	}
	claim.Status = nextStatus
	claim.Message = message
	claim.Version++
	return nil
}

func claimMatchesFilter(claim *storagev1.VolumeClaim, filter *storagev1.VolumeClaimListFilter) bool {
	if filter == nil {
		return claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED
	}
	if filter.GetNamespace() != "" && claim.GetNamespace() != filter.GetNamespace() {
		return false
	}
	if len(filter.GetStatuses()) > 0 {
		matched := false
		for _, status := range filter.GetStatuses() {
			if claim.GetStatus() == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	} else if claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return false
	}
	for key, value := range filter.GetLabels() {
		if claim.GetLabels()[key] != value {
			return false
		}
	}
	return true
}
