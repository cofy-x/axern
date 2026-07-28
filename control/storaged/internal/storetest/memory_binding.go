package storetest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *MemoryStore) GetVolumeBindingForClaim(_ context.Context, claimID string) (*privatestoragev1.VolumeBinding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	for _, binding := range s.bindings {
		if binding.GetClaimID() == claimID && binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			return proto.Clone(binding).(*privatestoragev1.VolumeBinding), true, nil
		}
	}
	return nil, false, nil
}

func (s *MemoryStore) ReserveVolumeBinding(_ context.Context, req kernel.VolumeBindingReserve) (*privatestoragev1.ResolvedNodeVolume, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	claim := req.Claim
	class := req.Class
	mount := req.Mount
	if claim == nil || class == nil || mount == nil {
		return nil, fmt.Errorf("volume binding reserve requires claim, class, and mount")
	}
	if stored := s.claims[claim.GetNamespace()+"/"+claim.GetName()]; stored != nil && stored.GetTopology().GetNodeID() != "" && stored.GetTopology().GetNodeID() != req.NodeID {
		return nil, fmt.Errorf("volume claim %q is already bound to node %q", claim.GetID(), stored.GetTopology().GetNodeID())
	}
	bindingID := req.AllocationID + "/" + claim.GetName()
	if existing := s.bindings[bindingID]; existing != nil && existing.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		if existing.GetClaimID() != claim.GetID() || existing.GetAllocationID() != req.AllocationID || existing.GetNodeID() != req.NodeID {
			return nil, fmt.Errorf("volume binding %q already exists for a different allocation target", bindingID)
		}
		switch existing.GetStatus() {
		case storagev1.VolumeStatus_VOLUME_STATUS_BOUND, storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED:
			volume := resolvedNodeVolumeFromReserve(req, claim, class, mount, bindingID)
			if !resolvedNodeVolumesEqual(existing.GetResolvedVolume(), volume) {
				return nil, fmt.Errorf("volume binding %q already exists with a different resolved volume", bindingID)
			}
			return proto.Clone(existing.GetResolvedVolume()).(*privatestoragev1.ResolvedNodeVolume), nil
		case storagev1.VolumeStatus_VOLUME_STATUS_RELEASING:
			return nil, fmt.Errorf("volume binding %q is releasing", bindingID)
		}
	}
	for _, existing := range s.bindings {
		if existing.GetClaimID() == claim.GetID() && existing.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED && existing.GetResolvedVolume().GetTopology().GetNodeID() != "" && existing.GetResolvedVolume().GetTopology().GetNodeID() != req.NodeID {
			return nil, fmt.Errorf("volume claim %q is already bound to node %q", claim.GetID(), existing.GetResolvedVolume().GetTopology().GetNodeID())
		}
	}
	volume := resolvedNodeVolumeFromReserve(req, claim, class, mount, bindingID)
	stored := s.claims[claim.GetNamespace()+"/"+claim.GetName()]
	if stored != nil {
		if err := kernel.TransitionClaimStatus(stored.GetStatus(), storagev1.VolumeStatus_VOLUME_STATUS_BOUND); err != nil {
			return nil, err
		}
	}
	s.bindings[bindingID] = &privatestoragev1.VolumeBinding{
		BindingID:      bindingID,
		ClaimID:        claim.GetID(),
		Namespace:      req.Namespace,
		ClaimName:      claim.GetName(),
		WorkloadID:     req.WorkloadID,
		WorkloadType:   req.WorkloadType,
		AllocationID:   req.AllocationID,
		NodeID:         req.NodeID,
		Status:         storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
		ResolvedVolume: proto.Clone(volume).(*privatestoragev1.ResolvedNodeVolume),
	}
	if stored != nil {
		stored.Status = storagev1.VolumeStatus_VOLUME_STATUS_BOUND
		stored.Topology = &storagev1.VolumeTopology{NodeID: req.NodeID}
		stored.Version++
	}
	return proto.Clone(volume).(*privatestoragev1.ResolvedNodeVolume), nil
}

func resolvedNodeVolumeFromReserve(req kernel.VolumeBindingReserve, claim *storagev1.VolumeClaim, class *storagev1.VolumeClass, mount *privatestoragev1.WorkloadVolumeMount, bindingID string) *privatestoragev1.ResolvedNodeVolume {
	volume := &privatestoragev1.ResolvedNodeVolume{
		ClaimID:       claim.GetID(),
		BindingID:     bindingID,
		VolumeID:      claim.GetName(),
		BackendHandle: claim.GetBackendHandle(),
		Backend:       class.GetBackend(),
		AccessMode:    claim.GetAccessMode(),
		Topology:      &storagev1.VolumeTopology{NodeID: req.NodeID},
		Target:        mount.GetTarget(),
		Readonly:      mount.GetReadonly(),
		Options:       append([]string(nil), mount.GetOptions()...),
		Parameters: map[string]string{
			"namespace":   req.Namespace,
			"service_id":  req.WorkloadID,
			"volume_name": claim.GetName(),
		},
		ConsistencyProfile:   class.GetConsistencyProfile(),
		RuntimeCompatibility: cloneRuntimeCompatibility(class.GetRuntimeCompatibility()),
	}
	for key, value := range class.GetParameters() {
		volume.Parameters[key] = value
	}
	for key, value := range claim.GetParameters() {
		volume.Parameters[key] = value
	}
	return volume
}

func resolvedNodeVolumesEqual(left, right *privatestoragev1.ResolvedNodeVolume) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.GetClaimID() == right.GetClaimID() &&
		left.GetBindingID() == right.GetBindingID() &&
		left.GetVolumeID() == right.GetVolumeID() &&
		left.GetBackendHandle() == right.GetBackendHandle() &&
		left.GetBackend() == right.GetBackend() &&
		left.GetAccessMode() == right.GetAccessMode() &&
		left.GetTopology().GetNodeID() == right.GetTopology().GetNodeID() &&
		left.GetTarget() == right.GetTarget() &&
		left.GetReadonly() == right.GetReadonly() &&
		slices.Equal(left.GetOptions(), right.GetOptions()) &&
		maps.Equal(left.GetParameters(), right.GetParameters()) &&
		left.GetConsistencyProfile() == right.GetConsistencyProfile() &&
		proto.Equal(left.GetRuntimeCompatibility(), right.GetRuntimeCompatibility())
}

func (s *MemoryStore) ReleaseVolumeBindings(_ context.Context, allocationID, nodeID string) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" {
		return fmt.Errorf("allocation id is required")
	}
	if nodeID == "" {
		return fmt.Errorf("node id is required")
	}
	return s.ReportVolumeRelease(context.Background(), allocationID, nodeID, nil)
}

func (s *MemoryStore) RetryFailedVolumeBinding(_ context.Context, bindingID, operatorReason string) (*privatestoragev1.VolumeBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	bindingID = strings.TrimSpace(bindingID)
	binding := s.bindings[bindingID]
	if binding == nil || binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil, fmt.Errorf("volume binding %q not found", bindingID)
	}
	if binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
		return nil, fmt.Errorf("volume binding %q is %s, want failed", bindingID, binding.GetStatus())
	}
	binding.Status = storagev1.VolumeStatus_VOLUME_STATUS_BOUND
	binding.Message = strings.TrimSpace(operatorReason)
	binding.PublishedVolume = nil
	binding.PublishedAt = nil
	binding.ReleasedAt = nil
	binding.UpdatedAt = timestamppb.Now()
	if err := s.recomputeClaimStatusLocked(binding.GetClaimID()); err != nil {
		return nil, err
	}
	return proto.Clone(binding).(*privatestoragev1.VolumeBinding), nil
}

func (s *MemoryStore) ListVolumeBindings(_ context.Context, filter kernel.VolumeBindingListFilter) ([]*privatestoragev1.VolumeBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	filter = kernel.NormalizeVolumeBindingListFilter(filter)
	if err := kernel.ValidateVolumeBindingListFilter(filter); err != nil {
		return nil, err
	}
	statuses := map[storagev1.VolumeStatus]struct{}{}
	for _, status := range filter.Statuses {
		statuses[status] = struct{}{}
	}
	out := make([]*privatestoragev1.VolumeBinding, 0, len(s.bindings))
	for _, binding := range s.bindings {
		if len(statuses) > 0 {
			if _, ok := statuses[binding.GetStatus()]; !ok {
				continue
			}
		} else if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			continue
		}
		if filter.Namespace != "" && binding.GetNamespace() != filter.Namespace {
			continue
		}
		if filter.ClaimName != "" && binding.GetClaimName() != filter.ClaimName {
			continue
		}
		if filter.WorkloadID != "" && binding.GetWorkloadID() != filter.WorkloadID {
			continue
		}
		if filter.AllocationID != "" && binding.GetAllocationID() != filter.AllocationID {
			continue
		}
		if filter.NodeID != "" && binding.GetNodeID() != filter.NodeID {
			continue
		}
		out = append(out, proto.Clone(binding).(*privatestoragev1.VolumeBinding))
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].GetUpdatedAt().AsTime()
		right := out[j].GetUpdatedAt().AsTime()
		if !left.Equal(right) {
			return left.After(right)
		}
		return out[i].GetBindingID() < out[j].GetBindingID()
	})
	if len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryStore) ReportVolumePublish(_ context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumePublishObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	for _, observation := range observations {
		if observation == nil {
			continue
		}
		if err := kernel.ValidateVolumePublishObservation(observation); err != nil {
			return err
		}
		binding := s.bindings[observation.GetBindingID()]
		if binding == nil || binding.GetAllocationID() != allocationID {
			return fmt.Errorf("volume binding %q not found", observation.GetBindingID())
		}
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			return fmt.Errorf("volume binding %q not found", observation.GetBindingID())
		}
		if nodeID != "" && binding.GetNodeID() != nodeID {
			return fmt.Errorf("volume binding %q not found", observation.GetBindingID())
		}
		binding.Status = observation.GetStatus()
		binding.Message = strings.TrimSpace(observation.GetMessage())
		if observation.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED {
			binding.PublishedVolume = proto.Clone(observation.GetPublishedVolume()).(*privatestoragev1.PublishedNodeVolume)
			binding.PublishedAt = timestamppb.Now()
		}
		if observation.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
			binding.PublishedVolume = nil
			binding.PublishedAt = nil
		}
		binding.UpdatedAt = timestamppb.Now()
		if err := s.updateClaimFromBindingLocked(binding); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) ReportVolumeRelease(_ context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	targets := map[string]*privatestoragev1.VolumeReleaseObservation{}
	for _, observation := range observations {
		if observation != nil {
			if err := kernel.ValidateVolumeReleaseObservation(observation); err != nil {
				return err
			}
			targets[observation.GetBindingID()] = observation
		}
	}
	for bindingID := range targets {
		binding := s.bindings[bindingID]
		if binding == nil || binding.GetAllocationID() != allocationID {
			return fmt.Errorf("volume binding %q not found", bindingID)
		}
		if nodeID != "" && binding.GetNodeID() != nodeID {
			return fmt.Errorf("volume binding %q not found", bindingID)
		}
	}
	for bindingID, binding := range s.bindings {
		if binding.GetAllocationID() != allocationID {
			continue
		}
		if nodeID != "" && binding.GetNodeID() != nodeID {
			continue
		}
		observation, filtered := targets[bindingID]
		if len(targets) > 0 && !filtered {
			continue
		}
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			if observation != nil && observation.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
				continue
			}
			if observation == nil {
				continue
			}
			return fmt.Errorf("volume binding %q is already deleted", bindingID)
		}
		if observation != nil {
			binding.Status = observation.GetStatus()
			binding.Message = strings.TrimSpace(observation.GetMessage())
		} else {
			binding.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
			binding.Message = ""
		}
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			binding.ReleasedAt = timestamppb.Now()
		}
		if binding.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_FAILED {
			binding.ReleasedAt = nil
		}
		binding.UpdatedAt = timestamppb.Now()
		if err := s.recomputeClaimStatusLocked(binding.GetClaimID()); err != nil {
			return err
		}
	}
	return nil
}
