package apprun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Reconciler interface {
	ReconcilePending(ctx context.Context, now time.Time) error
	WaitForWork(ctx context.Context) error
}

func NewReconciler(store runkernel.ReconcileStore, lifecycle AllocationLifecycle, owner string, now func() time.Time) Reconciler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &reconciler{
		store:        store,
		lifecycle:    lifecycle,
		owner:        strings.TrimSpace(owner),
		claimTTL:     allocationkernel.ReconcileClaimTTL,
		claimRenewal: allocationkernel.ReconcileClaimRenewal,
		workerCount:  allocationkernel.ReconcileWorkerCount,
		now:          now,
	}
}

type reconciler struct {
	store        runkernel.ReconcileStore
	lifecycle    AllocationLifecycle
	owner        string
	runMu        sync.Mutex
	claimTTL     time.Duration
	claimRenewal time.Duration
	workerCount  int
	now          func() time.Time
}

func (r *reconciler) ReconcilePending(ctx context.Context, now time.Time) error {
	if r.store == nil || r.lifecycle == nil {
		return nil
	}
	if r.owner == "" {
		return errors.New("allocation reconcile owner is required")
	}
	r.runMu.Lock()
	defer r.runMu.Unlock()
	var result error
	for {
		queryCtx, cancel := context.WithTimeout(ctx, allocationkernel.LifecycleOperationTimeout)
		items, err := r.store.ClaimDueReconcileItems(queryCtx, r.owner, r.workerCount, now, r.claimTTL)
		cancel()
		if err != nil {
			return errors.Join(result, err)
		}
		if len(items) == 0 {
			return result
		}
		var wg sync.WaitGroup
		errs := make(chan error, len(items))
		for _, item := range items {
			item := item
			wg.Add(1)
			go func() {
				defer wg.Done()
				if itemErr := r.reconcileClaimedAllocation(ctx, item); itemErr != nil {
					errs <- itemErr
				}
			}()
		}
		wg.Wait()
		close(errs)
		for itemErr := range errs {
			result = errors.Join(result, itemErr)
		}
	}
}

func (r *reconciler) WaitForWork(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	return r.store.WaitReconcileWork(ctx)
}

func (r *reconciler) reconcileClaimedAllocation(ctx context.Context, item allocationkernel.ReconcileItem) error {
	timeout := allocationkernel.LifecycleOperationTimeout
	if allocationkernel.ReconcileIntentForLifecycle(item.LifecycleState) == allocationkernel.ReconcileIntentEnsurePresent {
		timeout = allocationkernel.CreateExecutionTimeout
	}
	itemCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stopRenewal := make(chan struct{})
	renewalDone := make(chan error, 1)
	go r.renewClaim(itemCtx, cancel, item, stopRenewal, renewalDone)
	err := r.reconcileAllocation(itemCtx, item)
	close(stopRenewal)
	renewalErr := <-renewalDone
	if err == nil {
		// A renewal can race with successful completion, which atomically
		// removes the queue row. Once completion commits, a missing claim is
		// the expected result rather than a failed operation.
		return nil
	}
	return errors.Join(err, renewalErr)
}

func (r *reconciler) renewClaim(ctx context.Context, cancelOperation context.CancelFunc, item allocationkernel.ReconcileItem, stop <-chan struct{}, done chan<- error) {
	ticker := time.NewTicker(r.claimRenewal)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, r.claimRenewal)
			held, err := r.store.RenewReconcileClaim(renewCtx, item.AllocationID, item.ClaimOwner, r.now().UTC(), r.claimTTL)
			cancel()
			if err != nil {
				cancelOperation()
				done <- fmt.Errorf("renew allocation %s reconcile claim: %w", item.AllocationID, err)
				return
			}
			if !held {
				cancelOperation()
				done <- allocationkernel.ErrReconcileClaimLost
				return
			}
		}
	}
}

func (r *reconciler) reconcileAllocation(ctx context.Context, item allocationkernel.ReconcileItem) error {
	switch allocationkernel.ReconcileIntentForLifecycle(item.LifecycleState) {
	case allocationkernel.ReconcileIntentEnsurePresent:
		return r.reconcileStart(ctx, item)
	case allocationkernel.ReconcileIntentEnsureAbsent:
		return r.reconcileDeleteRetry(ctx, item)
	}
	return fmt.Errorf("allocation %s has non-reconcilable lifecycle state %s", item.AllocationID, item.LifecycleState)
}

func (r *reconciler) reconcileStart(ctx context.Context, item allocationkernel.ReconcileItem) error {
	start, err := r.store.LoadStartAllocation(ctx, item.AllocationID)
	if err != nil {
		if grpcstatus.Code(err) == codes.NotFound {
			return r.store.CompleteAllocationStart(ctx, item.AllocationID, item.ClaimOwner, nil, r.now().UTC())
		}
		return err
	}
	if start == nil || start.Run == nil || start.Environment == nil || start.Allocation == nil || runkernel.IsTerminal(start.Run.GetStatus()) {
		return r.store.CompleteAllocationStart(ctx, item.AllocationID, item.ClaimOwner, nil, r.now().UTC())
	}
	conditions, err := r.lifecycle.CreateAllocation(ctx, start.Allocation.NodeTarget, start.Run, start.Environment, start.Allocation.NodeID, start.Allocation.CapabilityRequirements)
	if err != nil {
		actionNow := r.now().UTC()
		if req, ok := allocationkernel.ScheduleCreateRetryRequest(item.AllocationID, item.ReconcileAttempts, err.Error(), actionNow); ok {
			updated, scheduleErr := r.store.ScheduleClaimedReconcile(ctx, req, item.ClaimOwner, actionNow)
			return claimedUpdateError(updated, scheduleErr)
		}
		if _, markErr := r.store.MarkAllocationCreateFailed(ctx, start.Allocation.AllocationID, item.ClaimOwner, err.Error(), actionNow); markErr != nil {
			return markErr
		}
		return nil
	}
	return r.store.CompleteAllocationStart(ctx, item.AllocationID, item.ClaimOwner, conditions, r.now().UTC())
}

func (r *reconciler) reconcileDeleteRetry(ctx context.Context, item allocationkernel.ReconcileItem) error {
	err := r.lifecycle.DeleteAllocation(ctx, item.NodeTarget, item.AllocationID, item.NodeID)
	if err != nil {
		actionNow := r.now().UTC()
		updated, scheduleErr := r.store.ScheduleClaimedReconcile(ctx, allocationkernel.ScheduleDeleteRetryRequest(item.AllocationID, err.Error(), actionNow), item.ClaimOwner, actionNow)
		return claimedUpdateError(updated, scheduleErr)
	}
	return r.store.CompleteAllocationRelease(ctx, item.AllocationID, item.ClaimOwner, r.now().UTC())
}

func claimedUpdateError(updated bool, err error) error {
	if err != nil {
		return err
	}
	if !updated {
		return allocationkernel.ErrReconcileClaimLost
	}
	return nil
}
