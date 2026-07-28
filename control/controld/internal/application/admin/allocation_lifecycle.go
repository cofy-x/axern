package appadmin

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
)

type AllocationLifecycleStore interface {
	ListAllocationLifecycleRetries(ctx context.Context, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error)
	ForceAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ForceLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
	FailAllocationLifecycleRetry(ctx context.Context, req allocationkernel.FailLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
	ClearAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ClearLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error)
}

type AllocationLifecycleControl struct {
	store AllocationLifecycleStore
}

func NewAllocationLifecycleControl(store AllocationLifecycleStore) AllocationLifecycleControl {
	return AllocationLifecycleControl{store: store}
}

func (c AllocationLifecycleControl) ListAllocationLifecycleRetries(ctx context.Context, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error) {
	filter = allocationkernel.NormalizeLifecycleRetryFilter(filter)
	if err := allocationkernel.ValidateLifecycleRetryFilter(filter); err != nil {
		return nil, err
	}
	return c.store.ListAllocationLifecycleRetries(ctx, filter, now)
}

func (c AllocationLifecycleControl) ForceAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ForceLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeForceLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateForceLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	return c.store.ForceAllocationLifecycleRetry(ctx, req, now)
}

func (c AllocationLifecycleControl) FailAllocationLifecycleRetry(ctx context.Context, req allocationkernel.FailLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeFailLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateFailLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	return c.store.FailAllocationLifecycleRetry(ctx, req, now)
}

func (c AllocationLifecycleControl) ClearAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ClearLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeClearLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateClearLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	return c.store.ClearAllocationLifecycleRetry(ctx, req, now)
}
