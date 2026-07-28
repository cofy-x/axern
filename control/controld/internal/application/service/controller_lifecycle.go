package appservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (c *controller) markDegraded(serviceID, message string, now time.Time) (*servicev1.Service, error) {
	return c.statuses.UpdateStatus(context.Background(), serviceID, servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED, message, now)
}

func (c *controller) markAllocationCreateFailed(current *servicev1.Service, allocationID, message string, now time.Time) *servicev1.Service {
	next, err := c.allocations.MarkAllocationCreateFailed(context.Background(), current.GetID(), allocationID, message, now)
	if err == nil && next != nil {
		return next
	}
	return current
}

func (c *controller) reportStoragePublished(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.PublishedNodeVolume) error {
	if c.storage == nil || len(volumes) == 0 {
		return nil
	}
	return c.storage.ReportBindingPublish(ctx, allocationID, nodeID, volumes)
}

func (c *controller) reportStoragePublishFailed(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.ResolvedNodeVolume, message string) error {
	if c.storage == nil || len(volumes) == 0 {
		return nil
	}
	return c.storage.ReportBindingPublishFailed(ctx, allocationID, nodeID, volumes, message)
}

func (c *controller) reportStorageReleased(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error {
	if c.storage == nil {
		return nil
	}
	return c.storage.ReportBindingRelease(ctx, allocationID, nodeID, observations)
}

func storagePublishFailureMessage(volumes []*privatestoragev1.ResolvedNodeVolume, err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(volumes) == 0 {
		return message
	}
	lower := strings.ToLower(message)
	if strings.HasPrefix(lower, "volume publish failed:") {
		return message
	}
	if !strings.Contains(lower, "volume") && !strings.Contains(lower, "volumed") {
		return message
	}
	return fmt.Sprintf("volume publish failed: %s", message)
}

func (c *controller) deleteAndConfirmAllocation(ctx context.Context, alloc *servicekernel.AllocationRecord) (bool, error) {
	if alloc == nil {
		return true, nil
	}
	releaseObservations, deleteErr := c.lifecycle.DeleteResolvedAllocation(ctx, alloc.NodeTarget, alloc.AllocationID, alloc.Attempt, alloc.NodeID)
	if deleteErr != nil && !allocationDeleteMayHaveSucceeded(deleteErr) {
		return false, deleteErr
	}
	deleted, err := c.lifecycle.AllocationDeleted(ctx, alloc.NodeTarget, alloc.AllocationID, alloc.Attempt, alloc.NodeID)
	if err != nil {
		if deleteErr != nil {
			return false, deleteErr
		}
		return false, err
	}
	if !deleted {
		if deleteErr != nil {
			return false, deleteErr
		}
		return false, allocationStillExistsAfterDeleteError(alloc.AllocationID)
	}
	if deleteErr != nil {
		return false, deleteErr
	}
	if err := c.reportStorageReleased(ctx, alloc.AllocationID, alloc.NodeID, releaseObservations); err != nil {
		return false, fmt.Errorf("volume release failed: %w", err)
	}
	return true, nil
}

func allocationDeleteMayHaveSucceeded(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

func allocationStillExistsAfterDeleteError(allocationID string) error {
	return grpcstatus.Errorf(codes.FailedPrecondition, "allocation %q still exists on node after delete", allocationID)
}
