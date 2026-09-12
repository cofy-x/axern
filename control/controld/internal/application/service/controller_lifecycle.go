package appservice

import (
	"context"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
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

func (c *controller) deleteAndConfirmAllocation(ctx context.Context, alloc *servicekernel.AllocationRecord) (bool, error) {
	if alloc == nil {
		return true, nil
	}
	deleteErr := c.lifecycle.DeleteResolvedAllocation(ctx, alloc.NodeTarget, alloc.AllocationID, alloc.Attempt, alloc.NodeID)
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
