package storage

import (
	"context"
	"fmt"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) CreateVolumeClaim(ctx context.Context, req *storagev1.CreateVolumeClaimRequest) (*storagev1.VolumeClaim, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	namespace := kernel.NormalizeNamespace(req.GetNamespace())
	className := strings.TrimSpace(req.GetClassName())
	class, ok, err := c.store.GetVolumeClass(ctx, className)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("volume class %q not found", className)
	}
	params := kernel.VolumeClaimCreate{
		Namespace:         namespace,
		Name:              strings.TrimSpace(req.GetName()),
		ClassName:         className,
		RequestedCapacity: req.GetRequestedCapacity().GetSizeBytes(),
		AccessMode:        req.GetAccessMode(),
		ReclaimPolicy:     req.GetReclaimPolicy(),
		BindingScope:      req.GetBindingScope(),
		Parameters:        cloneStringMap(req.GetParameters()),
		Labels:            cloneStringMap(req.GetLabels()),
	}
	if err := kernel.ValidateVolumeClaimCreate(params, class); err != nil {
		return nil, err
	}
	now := timestamppb.New(c.now().UTC())
	claimID := "claim-" + uuid.NewString()
	return c.store.CreateVolumeClaim(ctx, &storagev1.VolumeClaim{
		ID:                claimID,
		Namespace:         namespace,
		Name:              params.Name,
		ClassName:         params.ClassName,
		RequestedCapacity: &storagev1.VolumeCapacity{SizeBytes: params.RequestedCapacity},
		AccessMode:        params.AccessMode,
		ReclaimPolicy:     params.ReclaimPolicy,
		BindingScope:      params.BindingScope,
		Status:            storagev1.VolumeStatus_VOLUME_STATUS_PENDING,
		Parameters:        params.Parameters,
		Labels:            params.Labels,
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
		BackendHandle:     claimID,
	})
}

func (c *Controller) GetVolumeClaim(ctx context.Context, namespace, name string) (*storagev1.VolumeClaim, bool, error) {
	if c == nil || c.store == nil {
		return nil, false, fmt.Errorf("storage controller store is required")
	}
	return c.store.GetVolumeClaim(ctx, kernel.NormalizeNamespace(namespace), strings.TrimSpace(name))
}

func (c *Controller) ListVolumeClaims(ctx context.Context, filter *storagev1.VolumeClaimListFilter) ([]*storagev1.VolumeClaim, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	return c.store.ListVolumeClaims(ctx, filter)
}

func (c *Controller) UpdateVolumeClaim(ctx context.Context, req *storagev1.UpdateVolumeClaimRequest) (*storagev1.VolumeClaim, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	namespace := kernel.NormalizeNamespace(req.GetNamespace())
	name := strings.TrimSpace(req.GetName())
	if !kernel.StableName(name) {
		return nil, fmt.Errorf("volume claim name is required")
	}
	paths := updateMaskPaths(req)
	if err := validateUpdateMaskPaths(paths); err != nil {
		return nil, err
	}
	return c.store.UpdateVolumeClaim(ctx, namespace, name, req.GetExpectedVersion(), func(claim *storagev1.VolumeClaim) error {
		if shouldUpdate(paths, "requested_capacity") {
			if req.GetRequestedCapacity().GetSizeBytes() < 0 {
				return fmt.Errorf("volume claim requested capacity must be non-negative")
			}
			claim.RequestedCapacity = &storagev1.VolumeCapacity{SizeBytes: req.GetRequestedCapacity().GetSizeBytes()}
		}
		if shouldUpdate(paths, "labels") {
			claim.Labels = cloneStringMap(req.GetLabels())
		}
		claim.Version++
		claim.UpdatedAt = timestamppb.New(c.now().UTC())
		return nil
	})
}

func (c *Controller) DeleteVolumeClaim(ctx context.Context, req *storagev1.DeleteVolumeClaimRequest) (*storagev1.VolumeClaim, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("storage controller store is required")
	}
	namespace := kernel.NormalizeNamespace(req.GetNamespace())
	name := strings.TrimSpace(req.GetName())
	if !kernel.StableName(name) {
		return nil, fmt.Errorf("volume claim name is required")
	}
	claim, ok, err := c.store.GetVolumeClaim(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("volume claim %q/%q not found", namespace, name)
	}
	if binding, active, err := c.store.GetVolumeBindingForClaim(ctx, claim.GetID()); err != nil {
		return nil, err
	} else if active {
		return nil, fmt.Errorf("volume claim %q still has active binding %q", claim.GetID(), binding.GetBindingID())
	}
	return c.store.UpdateVolumeClaim(ctx, namespace, name, claim.GetVersion(), func(claim *storagev1.VolumeClaim) error {
		if claim.GetReclaimPolicy() == storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_DELETE && strings.TrimSpace(claim.GetTopology().GetNodeID()) != "" {
			claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETING
			claim.Message = "volume reclaim pending"
		} else {
			claim.Status = storagev1.VolumeStatus_VOLUME_STATUS_DELETED
			claim.Message = "volume claim deleted; backend retained"
		}
		claim.Version++
		claim.UpdatedAt = timestamppb.New(c.now().UTC())
		return nil
	})
}
