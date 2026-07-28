package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

const serviceReconcileStageEventQueueWait = "event_queue_wait"
const allocationReconcilePollInterval = time.Second
const defaultFunctionInvocationWorkers = 16
const functionInvocationExpiryBatchSize = 100
const functionInvocationExpiryBudget = time.Second

type functionInvocationExpirer interface {
	ExpireAsyncInvocations(context.Context, int) (int, error)
}

func (a *App) startReconciler() {
	a.startPeriodicReconciler()
	a.startServiceReconcileWorkers()
	a.startAllocationReconciler()
	a.startFunctionInvocationWorkers()
	a.startVolumeReclaimDispatcher()
}

func (a *App) startVolumeReclaimDispatcher() {
	if a.volumeReclaimWorker == nil || a.storage == nil {
		return
	}
	owner := "controld-volume-reclaim-" + uuid.NewString()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.volumeReclaimWorker.RunVolumeReclaimDispatcher(a.reconcileCtx, owner, a.volumeReclaimWorkers, a.volumeReclaimWorkersPerNode)
	}()
}

func (a *App) startFunctionInvocationWorkers() {
	if a.functionController == nil || a.functionPG == nil || a.functionInvoker == nil {
		return
	}
	owner := "controld-" + uuid.NewString()
	workers := a.functionInvocationWorkers
	if workers <= 0 {
		workers = defaultFunctionInvocationWorkers
	}
	for worker := 0; worker < workers; worker++ {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.functionController.RunAsyncDispatcher(a.reconcileCtx, owner)
		}()
	}
}

func (a *App) startPeriodicReconciler() {
	a.startPeriodicComponent(reconcilekernel.ComponentRollout, a.rolloutPG != nil, a.reconcileRolloutMaintenance)
	a.startPeriodicComponent(reconcilekernel.ComponentRun, a.runReconciler != nil, func(ctx context.Context, now time.Time) error {
		return a.runReconciler.ReconcilePending(ctx, now)
	})
	a.startPeriodicComponent(reconcilekernel.ComponentNode, a.nodeReconciler != nil, func(ctx context.Context, now time.Time) error {
		return a.nodeReconciler.ReconcileUnavailableNodes(ctx, now)
	})
	a.startPeriodicServiceReconciler()
	a.startPeriodicComponent(reconcilekernel.ComponentTunnel, a.tunnelPG != nil, func(ctx context.Context, now time.Time) error {
		return a.tunnelPG.ReconcileExpired(ctx, now)
	})
	a.startPeriodicComponent(reconcilekernel.ComponentFunction, a.functionController != nil && a.functionPG != nil, a.reconcileFunctionMaintenance)
}

func (a *App) startPeriodicComponent(component string, enabled bool, reconcile func(context.Context, time.Time) error) {
	if !enabled || reconcile == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(a.reconcileInterval)
		defer ticker.Stop()
		lifecycleDone := a.backgroundReconcileContext().Done()
		for {
			select {
			case <-ticker.C:
				a.reconcileComponent(component, a.now(), reconcile)
			case <-lifecycleDone:
				return
			case <-a.stopCh:
				return
			}
		}
	}()
}

func (a *App) startPeriodicServiceReconciler() {
	if a.serviceReconciler == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		autoscalingTicker := time.NewTicker(a.reconcileInterval)
		defer autoscalingTicker.Stop()
		recoveryInterval := a.serviceRecoveryInterval
		if recoveryInterval <= 0 {
			recoveryInterval = defaultServiceRecoveryInterval
		}
		recoveryTicker := time.NewTicker(recoveryInterval)
		defer recoveryTicker.Stop()
		lifecycleDone := a.backgroundReconcileContext().Done()
		select {
		case <-lifecycleDone:
			return
		default:
		}
		a.reconcileServiceSweep(a.now())
		for {
			select {
			case <-autoscalingTicker.C:
				a.reconcileAutoscaledServices(a.now())
			case <-recoveryTicker.C:
				a.reconcileServiceSweep(a.now())
			case <-lifecycleDone:
				return
			case <-a.stopCh:
				return
			}
		}
	}()
}

func (a *App) startServiceReconcileWorkers() {
	if a.serviceReconciler == nil || a.pendingServiceReconcile == nil {
		return
	}
	workers := a.serviceReconcileWorkers
	if workers <= 0 {
		workers = defaultServiceReconcileWorkers
	}
	for range workers {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			lifecycleDone := a.backgroundReconcileContext().Done()
			for {
				select {
				case <-a.pendingServiceReconcile.Wake():
					a.reconcileServiceEvent(a.now())
				case <-lifecycleDone:
					return
				case <-a.stopCh:
					return
				}
			}
		}()
	}
}

func (a *App) startAllocationReconciler() {
	if a.allocationReconciler == nil || a.allocationReconcileWake == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(allocationReconcilePollInterval)
		defer ticker.Stop()
		lifecycleDone := a.backgroundReconcileContext().Done()
		select {
		case <-lifecycleDone:
			return
		default:
		}
		a.reconcileAllocationBatches(a.now())
		for {
			select {
			case <-a.allocationReconcileWake:
				a.reconcileAllocationBatches(a.now())
			case <-ticker.C:
				a.reconcileAllocationBatches(a.now())
			case <-lifecycleDone:
				return
			case <-a.stopCh:
				return
			}
		}
	}()
}

func (a *App) reconcileAllocationBatches(now time.Time) {
	for {
		processed := 0
		err := a.reconcileComponent(reconcilekernel.ComponentAllocation, now, func(ctx context.Context, _ time.Time) error {
			var err error
			processed, err = a.allocationReconciler.ReconcileAllocationBatch(ctx, a.now())
			return err
		})
		if err != nil || processed == 0 {
			return
		}
		now = a.now()
	}
}

func (a *App) reconcileV1() {
	a.reconcileComponents(true)
}

func (a *App) reconcilePeriodicV1() {
	a.reconcileComponents(false)
}

func (a *App) reconcileComponents(serviceRecovery bool) {
	if a.rolloutPG != nil {
		a.reconcileComponent(reconcilekernel.ComponentRollout, a.now(), a.reconcileRolloutMaintenance)
	}
	if a.runReconciler != nil {
		a.reconcileComponent(reconcilekernel.ComponentRun, a.now(), func(ctx context.Context, now time.Time) error {
			return a.runReconciler.ReconcilePending(ctx, now)
		})
	}
	if a.nodeReconciler != nil {
		a.reconcileComponent(reconcilekernel.ComponentNode, a.now(), func(ctx context.Context, now time.Time) error {
			return a.nodeReconciler.ReconcileUnavailableNodes(ctx, now)
		})
	}
	if serviceRecovery {
		a.reconcileServiceSweep(a.now())
	} else {
		a.reconcileAutoscaledServices(a.now())
	}
	if a.tunnelPG != nil {
		a.reconcileComponent(reconcilekernel.ComponentTunnel, a.now(), func(ctx context.Context, now time.Time) error {
			return a.tunnelPG.ReconcileExpired(ctx, now)
		})
	}
	if a.functionController != nil && a.functionPG != nil {
		a.reconcileComponent(reconcilekernel.ComponentFunction, a.now(), a.reconcileFunctionMaintenance)
	}
}

func (a *App) reconcileRolloutMaintenance(ctx context.Context, now time.Time) error {
	if a.rolloutPG == nil {
		return nil
	}
	var result error
	if _, err := a.rolloutPG.ExpireBudgets(ctx, now); err != nil {
		result = errors.Join(result, fmt.Errorf("expire budgets: %w", err))
	}
	if _, err := a.rolloutPG.ReconcileExpiredLeases(ctx, now, 100); err != nil {
		result = errors.Join(result, fmt.Errorf("reconcile expired leases: %w", err))
	}
	if _, err := a.rolloutPG.ReconcileDoctorJobs(ctx, now, 100); err != nil {
		result = errors.Join(result, fmt.Errorf("reconcile doctor jobs: %w", err))
	}
	if _, err := a.rolloutPG.ReconcileDeletes(ctx, 32); err != nil {
		result = errors.Join(result, fmt.Errorf("reconcile deletes: %w", err))
	}
	return result
}

func (a *App) reconcileFunctionMaintenance(ctx context.Context, now time.Time) error {
	if a.functionController == nil || a.functionPG == nil {
		return nil
	}
	if err := expireAsyncInvocationBatches(ctx, a.functionPG); err != nil {
		return err
	}
	_, err := a.functionController.SweepIdleScaleDown(ctx, now)
	return err
}

func expireAsyncInvocationBatches(ctx context.Context, store functionInvocationExpirer) error {
	budgetCtx, cancel := context.WithTimeout(ctx, functionInvocationExpiryBudget)
	defer cancel()
	for {
		expired, err := store.ExpireAsyncInvocations(budgetCtx, functionInvocationExpiryBatchSize)
		if err != nil {
			if budgetCtx.Err() != nil {
				return nil
			}
			return err
		}
		if expired < functionInvocationExpiryBatchSize {
			return nil
		}
		select {
		case <-budgetCtx.Done():
			return nil
		default:
		}
	}
}

func (a *App) reconcileAutoscaledServices(now time.Time) {
	if a.serviceReconciler == nil {
		return
	}
	a.reconcileComponent(reconcilekernel.ComponentService, now, func(ctx context.Context, now time.Time) error {
		return a.serviceReconciler.ReconcileAutoscaled(ctx, now)
	})
}

func (a *App) reconcileServiceSweep(now time.Time) {
	if a.serviceReconciler == nil {
		return
	}
	a.reconcileComponent(reconcilekernel.ComponentService, now, func(ctx context.Context, now time.Time) error {
		return a.serviceReconciler.ReconcilePending(ctx, now)
	})
	a.notifyAllocationReconcile()
}

func (a *App) reconcileServiceEvent(now time.Time) {
	if a.serviceReconciler == nil || a.pendingServiceReconcile == nil {
		return
	}
	item := a.pendingServiceReconcile.Take()
	ctx := a.backgroundReconcileContext()
	if !item.EnqueuedAt.IsZero() {
		sdkobs.DurationHistogram(ctrlobs.MetricServiceReconcileStageDuration.Name, ctrlobs.MetricServiceReconcileStageDuration.Description).RecordDuration(
			ctx,
			time.Since(item.EnqueuedAt),
			attribute.String(sdkobs.AttrStage, serviceReconcileStageEventQueueWait),
			attribute.String(sdkobs.AttrResult, sdkobs.ResultOK),
			attribute.String(sdkobs.AttrErrorClass, ""),
		)
	}
	if item.FullSweep {
		sdkobs.Int64Counter(ctrlobs.MetricServiceReconcileQueueOverflowTotal.Name, ctrlobs.MetricServiceReconcileQueueOverflowTotal.Description).Add(ctx, 1)
		a.reconcileServiceSweep(now)
		return
	}
	if item.ServiceID == "" {
		return
	}
	a.reconcileComponent(reconcilekernel.ComponentService, now, func(ctx context.Context, now time.Time) error {
		return a.serviceReconciler.ReconcileServices(ctx, []string{item.ServiceID}, now)
	})
	a.notifyAllocationReconcile()
}

func (a *App) notifyServiceReconcile(serviceIDs ...string) {
	if a.pendingServiceReconcile != nil {
		a.pendingServiceReconcile.Enqueue(serviceIDs...)
	}
}

func (a *App) notifyAllocationReconcile() {
	if a.allocationReconcileWake == nil {
		return
	}
	select {
	case a.allocationReconcileWake <- struct{}{}:
	default:
	}
}

func (a *App) reconcileComponent(component string, now time.Time, reconcile func(context.Context, time.Time) error) error {
	var run reconcilekernel.RunHandle
	if a.reconcileHealth != nil {
		run = a.reconcileHealth.RecordStart(component, now)
	}
	ctx, cancel := context.WithTimeout(a.backgroundReconcileContext(), a.backgroundReconcileTimeout())
	err := reconcile(ctx, now)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	cancel()
	finishedAt := a.now()
	if a.reconcileHealth != nil {
		a.reconcileHealth.RecordResult(run, err, finishedAt)
	}
	logReconcileError(component, err)
	return err
}

func (a *App) backgroundReconcileContext() context.Context {
	if a != nil && a.reconcileCtx != nil {
		return a.reconcileCtx
	}
	return context.Background()
}

func (a *App) backgroundReconcileTimeout() time.Duration {
	if a != nil && a.reconcileTimeout > 0 {
		return a.reconcileTimeout
	}
	return defaultReconcileTimeout
}

func logReconcileError(component string, err error) {
	if err == nil {
		return
	}
	logrus.WithError(err).WithField("component", component).Warn("background reconcile failed")
}
