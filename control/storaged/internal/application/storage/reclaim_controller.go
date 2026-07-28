package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) DeleteWorkloadVolumeClaims(ctx context.Context, req *privatestoragev1.DeleteWorkloadVolumeClaimsRequest) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error) {
	namespace := strings.TrimSpace(req.GetNamespace())
	workloadID := strings.TrimSpace(req.GetWorkloadID())
	if namespace == "" || workloadID == "" {
		return nil, fmt.Errorf("storage delete workload namespace and workload id are required")
	}
	claims, err := c.store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{Namespace: namespace})
	if err != nil {
		return nil, err
	}
	now := c.now().UTC()
	result := &privatestoragev1.DeleteWorkloadVolumeClaimsResponse{Complete: true}
	for _, candidate := range claims {
		if candidate.GetOwnerType() != "service" || candidate.GetOwnerID() != workloadID || candidate.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			continue
		}
		result.ClaimIds = append(result.ClaimIds, candidate.GetID())
		result.Complete = false
		if binding, ok, err := c.store.GetVolumeBindingForClaim(ctx, candidate.GetID()); err != nil {
			return nil, err
		} else if ok {
			return nil, fmt.Errorf("volume claim %q still has active binding %q", candidate.GetID(), binding.GetBindingID())
		}
		claim := candidate
		if claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETING {
			claim, err = c.store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
				next.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETING
				next.Message = "volume reclaim pending"
				next.Version++
				next.UpdatedAt = timestamppb.New(now)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		if claim.GetReclaimPolicy() == storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_RETAIN || strings.TrimSpace(claim.GetTopology().GetNodeID()) == "" {
			if _, err := c.completeClaimDeletion(ctx, claim, now, "volume claim deleted; backend retained"); err != nil {
				return nil, err
			}
			continue
		}
	}
	if !result.Complete {
		remaining, err := c.workloadHasDeletingClaims(ctx, namespace, workloadID)
		if err != nil {
			return nil, err
		}
		result.Complete = !remaining
	}
	return result, nil
}

func (c *Controller) ReportVolumeReclaim(ctx context.Context, req *privatestoragev1.ReportVolumeReclaimRequest) (*privatestoragev1.ReportVolumeReclaimResponse, error) {
	if err := c.store.ReportVolumeReclaim(ctx, req); err != nil {
		return nil, err
	}
	return &privatestoragev1.ReportVolumeReclaimResponse{}, nil
}

func (c *Controller) ListVolumeReclaims(ctx context.Context, req *privatestoragev1.ListVolumeReclaimsRequest) (*privatestoragev1.ListVolumeReclaimsResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	claims, err := c.store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{
		Namespace: strings.TrimSpace(req.GetNamespace()),
		Statuses:  []storagev1.VolumeStatus{storagev1.VolumeStatus_VOLUME_STATUS_DELETING},
	})
	if err != nil {
		return nil, err
	}
	out := &privatestoragev1.ListVolumeReclaimsResponse{}
	for _, claim := range claims {
		if strings.TrimSpace(req.GetWorkloadID()) != "" && claim.GetOwnerID() != strings.TrimSpace(req.GetWorkloadID()) {
			continue
		}
		if strings.TrimSpace(req.GetNodeID()) != "" && claim.GetTopology().GetNodeID() != strings.TrimSpace(req.GetNodeID()) {
			continue
		}
		out.Reclaims = append(out.Reclaims, &privatestoragev1.VolumeReclaim{
			ClaimID: claim.GetID(), Namespace: claim.GetNamespace(), ClaimName: claim.GetName(), WorkloadID: claim.GetOwnerID(),
			NodeID: claim.GetTopology().GetNodeID(), BackendHandle: claim.GetBackendHandle(), Attempt: claim.GetReclaimAttempt(),
			LastError: claim.GetMessage(), NextRetryAt: claim.GetNextReclaimAt(), UpdatedAt: claim.GetUpdatedAt(),
		})
		if len(out.Reclaims) >= limit {
			break
		}
	}
	return out, nil
}

// ClaimVolumeReclaims leases durable, due reclaim work for the control-plane
// worker. Listing remains read-only so operator inspection can never delay a
// reclaim or take ownership of a task.
func (c *Controller) ClaimVolumeReclaims(ctx context.Context, req *privatestoragev1.ClaimVolumeReclaimsRequest) (*privatestoragev1.ClaimVolumeReclaimsResponse, error) {
	reclaims, err := c.store.ClaimVolumeReclaims(ctx, req)
	return &privatestoragev1.ClaimVolumeReclaimsResponse{Reclaims: reclaims}, err
}

func (c *Controller) completeClaimDeletion(ctx context.Context, claim *storagev1.VolumeClaim, now time.Time, message string) (*storagev1.VolumeClaim, error) {
	return c.store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
		next.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
		next.Message = message
		next.NextReclaimAt = nil
		next.ReclaimLeaseToken = ""
		next.ReclaimLeaseUntil = nil
		next.Version++
		next.UpdatedAt = timestamppb.New(now)
		return nil
	})
}

func (c *Controller) workloadHasDeletingClaims(ctx context.Context, namespace, workloadID string) (bool, error) {
	claims, err := c.store.ListVolumeClaims(ctx, &storagev1.VolumeClaimListFilter{Namespace: namespace})
	if err != nil {
		return false, err
	}
	for _, claim := range claims {
		if claim.GetOwnerID() == workloadID && claim.GetOwnerType() == "service" && claim.GetStatus() != storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
			return true, nil
		}
	}
	return false, nil
}
