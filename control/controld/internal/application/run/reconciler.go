package apprun

import (
	"context"
	"errors"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Reconciler interface {
	ReconcilePending(ctx context.Context, now time.Time) error
}

func NewReconciler(store runkernel.ReconcileStore, lifecycle AllocationLifecycle) Reconciler {
	return reconciler{store: store, lifecycle: lifecycle}
}

type reconciler struct {
	store     runkernel.ReconcileStore
	lifecycle AllocationLifecycle
}

func (r reconciler) ReconcilePending(ctx context.Context, now time.Time) error {
	if r.store == nil || r.lifecycle == nil {
		return nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, allocationkernel.LifecycleOperationTimeout)
	items, err := r.store.DueReconcileItems(queryCtx, allocationkernel.DefaultReconcileLimit, now)
	cancel()
	if err != nil {
		return err
	}
	for _, item := range items {
		timeout := allocationkernel.LifecycleOperationTimeout
		if item.Reason == allocationkernel.ReconcileReasonCreate {
			timeout = allocationkernel.CreateExecutionTimeout
		}
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		err = errors.Join(err, r.reconcileAllocation(itemCtx, item, now))
		cancel()
	}
	return err
}

func (r reconciler) reconcileAllocation(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	switch item.Reason {
	case allocationkernel.ReconcileReasonCreate:
		return r.reconcileStart(ctx, item, now)
	case allocationkernel.ReconcileReasonDelete:
		return r.reconcileDeleteRetry(ctx, item, now)
	}
	return nil
}

func (r reconciler) reconcileStart(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	start, err := r.store.LoadStartAllocation(ctx, item.AllocationID)
	if err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return r.store.CompleteAllocationStart(ctx, item.AllocationID, now)
		}
		return err
	}
	if start == nil || start.Run == nil || start.Environment == nil || start.Allocation == nil || runkernel.IsTerminal(start.Run.GetStatus()) {
		return r.store.CompleteAllocationStart(ctx, item.AllocationID, now)
	}
	admission, err := r.lifecycle.CreateAllocation(ctx, start.Allocation.NodeTarget, start.Run, start.Environment, start.Allocation.NodeID, start.Allocation.CapabilityDependencies)
	if err != nil {
		if req, ok := allocationkernel.ScheduleCreateRetryRequest(item.AllocationID, item.ReconcileAttempts, err.Error(), now); ok {
			_, err := r.store.RescheduleReconcile(ctx, req, now)
			return err
		}
		if _, markErr := r.store.MarkAllocationCreateFailed(ctx, start.Allocation.AllocationID, err.Error(), now); markErr != nil {
			return markErr
		}
		return r.store.CompleteAllocationStart(ctx, item.AllocationID, now)
	}
	if err := r.store.RecordAllocationCapabilityAdmission(ctx, start.Allocation.AllocationID, admission, now); err != nil {
		return err
	}
	return r.store.CompleteAllocationStart(ctx, item.AllocationID, now)
}

func (r reconciler) reconcileDeleteRetry(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	err := r.lifecycle.DeleteAllocation(ctx, item.NodeTarget, item.AllocationID, item.Attempt, item.NodeID)
	if err != nil {
		_, scheduleErr := r.store.RescheduleReconcile(ctx, allocationkernel.ScheduleDeleteRetryRequest(item.AllocationID, err.Error(), now), now)
		return scheduleErr
	}
	return r.store.CompleteAllocationRelease(ctx, item.AllocationID, item.Attempt, now)
}
