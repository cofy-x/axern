package storage

import (
	"context"
	"fmt"
	"strings"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) claimAndClass(ctx context.Context, namespace, claimName string) (*storagev1.VolumeClaim, *storagev1.VolumeClass, error) {
	claimName = strings.TrimSpace(claimName)
	if !kernel.StableName(claimName) {
		return nil, nil, fmt.Errorf("volume claim name is required")
	}
	claim, ok, err := c.store.GetVolumeClaim(ctx, namespace, claimName)
	if err != nil {
		return nil, nil, err
	}
	if !ok || claim.GetStatus() == storagev1.VolumeStatus_VOLUME_STATUS_DELETED {
		return nil, nil, fmt.Errorf("volume claim %q/%q not found", namespace, claimName)
	}
	class, ok, err := c.store.GetVolumeClass(ctx, claim.GetClassName())
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("volume class %q not found", claim.GetClassName())
	}
	return claim, class, nil
}

func (c *Controller) ensureClaimAndClass(ctx context.Context, namespace, workloadID, workloadType string, mount *privatestoragev1.WorkloadVolumeMount) (*storagev1.VolumeClaim, *storagev1.VolumeClass, error) {
	claimName := strings.TrimSpace(mount.GetClaimName())
	claim, class, err := c.claimAndClass(ctx, namespace, claimName)
	if err == nil {
		if claim.GetBindingScope() == storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE && claim.GetOwnerID() == "" {
			claim, err = c.store.UpdateVolumeClaim(ctx, claim.GetNamespace(), claim.GetName(), claim.GetVersion(), func(next *storagev1.VolumeClaim) error {
				next.OwnerID = strings.TrimSpace(workloadID)
				next.OwnerType = strings.TrimSpace(workloadType)
				if next.GetBackendHandle() == "" {
					next.BackendHandle = next.GetID()
				}
				next.Version++
				next.UpdatedAt = timestamppb.New(c.now().UTC())
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
		}
		if claim.GetBindingScope() == storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE &&
			(claim.GetOwnerID() != strings.TrimSpace(workloadID) || claim.GetOwnerType() != strings.TrimSpace(workloadType)) {
			return nil, nil, fmt.Errorf("volume claim %q/%q is owned by another workload", namespace, claimName)
		}
		return claim, class, nil
	}
	if !strings.Contains(err.Error(), "not found") {
		return nil, nil, err
	}
	class, ok, classErr := c.store.GetVolumeClass(ctx, "local")
	if classErr != nil {
		return nil, nil, classErr
	}
	if !ok {
		return nil, nil, err
	}
	reclaimPolicy := mount.GetReclaimPolicy()
	if reclaimPolicy == storagev1.VolumeReclaimPolicy_VOLUME_RECLAIM_POLICY_UNSPECIFIED {
		reclaimPolicy = class.GetDefaultReclaimPolicy()
	}
	now := timestamppb.New(c.now().UTC())
	claimID := "claim-" + uuid.NewString()
	claim, createErr := c.store.CreateVolumeClaim(ctx, &storagev1.VolumeClaim{
		ID: claimID, Namespace: namespace, Name: claimName, ClassName: class.GetName(),
		RequestedCapacity: &storagev1.VolumeCapacity{}, AccessMode: storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ReclaimPolicy: reclaimPolicy, BindingScope: storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE,
		Status: storagev1.VolumeStatus_VOLUME_STATUS_PENDING, Version: 1, CreatedAt: now, UpdatedAt: now,
		OwnerID: strings.TrimSpace(workloadID), OwnerType: strings.TrimSpace(workloadType), BackendHandle: claimID,
	})
	if createErr != nil {
		claim, class, err = c.claimAndClass(ctx, namespace, claimName)
		if err == nil {
			if claim.GetBindingScope() == storagev1.VolumeBindingScope_VOLUME_BINDING_SCOPE_SERVICE &&
				(claim.GetOwnerID() != strings.TrimSpace(workloadID) || claim.GetOwnerType() != strings.TrimSpace(workloadType)) {
				return nil, nil, fmt.Errorf("volume claim %q/%q is owned by another workload", namespace, claimName)
			}
			return claim, class, nil
		}
		return nil, nil, createErr
	}
	return claim, class, nil
}
