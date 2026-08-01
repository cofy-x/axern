package storetest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *MemoryStore) CreateVolumeClaim(_ context.Context, claim *storagev1.VolumeClaim) (*storagev1.VolumeClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	key := claim.GetNamespace() + "/" + claim.GetName()
	if existing := s.claims[key]; existing != nil {
		if existing.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			return nil, fmt.Errorf("volume claim %q already exists", key)
		}
		s.claimTombstones[existing.GetID()] = proto.Clone(existing).(*storagev1.VolumeClaim)
	}
	out := proto.Clone(claim).(*storagev1.VolumeClaim)
	s.claims[key] = out
	return proto.Clone(out).(*storagev1.VolumeClaim), nil
}

func (s *MemoryStore) ClaimVolumeReclaims(_ context.Context, req *privatestoragev1.ClaimVolumeReclaimsRequest) ([]*privatestoragev1.VolumeReclaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	owner := strings.TrimSpace(req.GetLeaseOwner())
	if owner == "" {
		return nil, fmt.Errorf("volume reclaim lease owner is required")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	excluded := map[string]struct{}{}
	for _, nodeID := range req.GetExcludedNodeIds() {
		excluded[nodeID] = struct{}{}
	}
	now := time.Now().UTC()
	keys := make([]string, 0, len(s.claims))
	for key := range s.claims {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*privatestoragev1.VolumeReclaim, 0, limit)
	for _, key := range keys {
		if len(out) >= limit {
			break
		}
		claim := s.claims[key]
		if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING || claim.GetReclaimPolicy() != storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE {
			continue
		}
		if _, skip := excluded[claim.GetTopology().GetNodeID()]; skip || claim.GetTopology().GetNodeID() == "" {
			continue
		}
		if claim.GetNextReclaimAt() != nil && claim.GetNextReclaimAt().AsTime().After(now) {
			continue
		}
		if claim.GetReclaimLeaseUntil() != nil && claim.GetReclaimLeaseUntil().AsTime().After(now) {
			continue
		}
		active := false
		for _, binding := range s.bindings {
			if binding.GetClaimID() == claim.GetID() && binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
				active = true
				break
			}
		}
		if active {
			continue
		}
		token := uuid.NewString()
		s.reclaimVersions[claim.GetID()]++
		s.reclaimOwners[claim.GetID()] = owner
		claim.ReclaimLeaseToken = token
		claim.ReclaimLeaseUntil = timestamppb.New(now.Add(60 * time.Second))
		claim.Version++
		claim.UpdatedAt = timestamppb.New(now)
		class := s.classes[claim.GetClassName()]
		out = append(out, &privatestoragev1.VolumeReclaim{
			ClaimID: claim.GetID(), Namespace: claim.GetNamespace(), ClaimName: claim.GetName(), WorkloadID: claim.GetOwnerID(),
			NodeID: claim.GetTopology().GetNodeID(), Backend: class.GetBackend(), BackendHandle: claim.GetBackendHandle(),
			Attempt: claim.GetReclaimAttempt() + 1, LastError: claim.GetMessage(), NextRetryAt: claim.GetNextReclaimAt(), UpdatedAt: claim.GetUpdatedAt(),
			LeaseToken: token, LeaseOwner: owner, LeaseGeneration: s.reclaimVersions[claim.GetID()],
		})
	}
	return out, nil
}

func (s *MemoryStore) ReportVolumeReclaim(_ context.Context, req *privatestoragev1.ReportVolumeReclaimRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	var claim *storagev1.VolumeClaim
	for _, candidate := range s.claims {
		if candidate.GetID() == req.GetClaimID() {
			claim = candidate
			break
		}
	}
	if claim == nil || claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil
	}
	now := time.Now().UTC()
	if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING || claim.GetTopology().GetNodeID() != req.GetNodeID() ||
		s.reclaimOwners[claim.GetID()] != req.GetLeaseOwner() || s.reclaimVersions[claim.GetID()] != req.GetLeaseGeneration() ||
		claim.GetReclaimLeaseToken() != req.GetLeaseToken() || claim.GetReclaimLeaseUntil() == nil || !claim.GetReclaimLeaseUntil().AsTime().After(now) {
		return fmt.Errorf("volume claim %q reclaim lease is stale or not owned by this worker", claim.GetID())
	}
	claim.ReclaimLeaseToken = ""
	claim.ReclaimLeaseUntil = nil
	delete(s.reclaimOwners, claim.GetID())
	claim.Version++
	claim.UpdatedAt = timestamppb.New(now)
	if req.GetSucceeded() {
		claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
		claim.Message = "volume backend reclaimed"
		claim.NextReclaimAt = nil
		return nil
	}
	claim.ReclaimAttempt++
	claim.Message = strings.TrimSpace(req.GetMessage())
	delay := 2 * time.Second * time.Duration(1<<min(claim.GetReclaimAttempt()-1, 4))
	claim.NextReclaimAt = timestamppb.New(now.Add(delay))
	return nil
}

func (s *MemoryStore) GetVolumeReclaimQueueHealth(_ context.Context) (*privatestoragev1.VolumeReclaimQueueHealth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	now := time.Now().UTC()
	health := &privatestoragev1.VolumeReclaimQueueHealth{}
	var oldest time.Time
	for _, claim := range s.claims {
		if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING || claim.GetReclaimPolicy() != storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE {
			continue
		}
		if claim.GetReclaimLeaseUntil() != nil {
			if claim.GetReclaimLeaseUntil().AsTime().After(now) {
				health.LeasedActive++
			} else {
				health.LeasedExpired++
			}
			continue
		}
		dueAt := claim.GetUpdatedAt().AsTime()
		if claim.GetNextReclaimAt() != nil {
			dueAt = claim.GetNextReclaimAt().AsTime()
		}
		if dueAt.After(now) {
			health.Scheduled++
		} else {
			health.Due++
			if oldest.IsZero() || dueAt.Before(oldest) {
				oldest = dueAt
			}
		}
	}
	if !oldest.IsZero() {
		health.OldestDueAgeSeconds = now.Sub(oldest).Seconds()
	}
	return health, nil
}

func (s *MemoryStore) GetVolumeClaim(_ context.Context, namespace, name string) (*storagev1.VolumeClaim, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	claim, ok := s.claims[namespace+"/"+name]
	if !ok {
		return nil, false, nil
	}
	return proto.Clone(claim).(*storagev1.VolumeClaim), true, nil
}

func (s *MemoryStore) ListVolumeClaims(_ context.Context, filter *storagev1.VolumeClaimListFilter) ([]*storagev1.VolumeClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	keys := make([]string, 0, len(s.claims)+len(s.claimTombstones))
	for key := range s.claims {
		keys = append(keys, "active/"+key)
	}
	for key := range s.claimTombstones {
		keys = append(keys, "deleted/"+key)
	}
	sort.Strings(keys)
	out := make([]*storagev1.VolumeClaim, 0, len(s.claims))
	for _, key := range keys {
		var claim *storagev1.VolumeClaim
		if len(key) > len("active/") && key[:len("active/")] == "active/" {
			claim = s.claims[key[len("active/"):]]
		} else {
			claim = s.claimTombstones[key[len("deleted/"):]]
		}
		if !claimMatchesFilter(claim, filter) {
			continue
		}
		out = append(out, proto.Clone(claim).(*storagev1.VolumeClaim))
	}
	return out, nil
}

func (s *MemoryStore) UpdateVolumeClaim(_ context.Context, namespace, name string, expectedVersion int64, mutate func(*storagev1.VolumeClaim) error) (*storagev1.VolumeClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	key := namespace + "/" + name
	current, ok := s.claims[key]
	if !ok {
		return nil, fmt.Errorf("volume claim %q not found", key)
	}
	if expectedVersion > 0 && current.GetVersion() != expectedVersion {
		return nil, fmt.Errorf("volume claim %q version mismatch: got %d, want %d", key, current.GetVersion(), expectedVersion)
	}
	next := proto.Clone(current).(*storagev1.VolumeClaim)
	if err := mutate(next); err != nil {
		return nil, err
	}
	s.claims[key] = next
	return proto.Clone(next).(*storagev1.VolumeClaim), nil
}

func (s *MemoryStore) ReleaseWorkloadVolumeClaims(_ context.Context, namespace, workloadID, workloadType string, now time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()

	keys := make([]string, 0)
	for key, claim := range s.claims {
		if claim.GetNamespace() == namespace && claim.GetOwnerID() == workloadID && claim.GetOwnerType() == workloadType &&
			claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		claim := s.claims[key]
		for _, binding := range s.bindings {
			if binding.GetClaimID() == claim.GetID() && binding.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
				return nil, fmt.Errorf("volume claim %q still has active binding %q", claim.GetID(), binding.GetBindingID())
			}
		}
	}

	releasedClaimIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		claim := s.claims[key]
		claim.OwnerID = ""
		claim.OwnerType = ""
		claim.Message = "volume claim owner released; backend retained"
		claim.Version++
		claim.UpdatedAt = timestamppb.New(now.UTC())
		releasedClaimIDs = append(releasedClaimIDs, claim.GetID())
	}
	return releasedClaimIDs, nil
}
