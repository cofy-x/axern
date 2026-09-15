package app

import (
	"context"
	"time"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	"github.com/sirupsen/logrus"
)

func (a *App) startReconciler() {
	a.startPeriodicReconciler()
}

func (a *App) startPeriodicReconciler() {
	a.startRunReconciler()
	a.startPeriodicComponent(reconcilekernel.ComponentNode, a.nodeReconciler != nil, func(ctx context.Context, now time.Time) error {
		return a.nodeReconciler.ReconcileUnavailableNodes(ctx, now)
	})
	a.startPeriodicComponent(reconcilekernel.ComponentTunnel, a.tunnelPG != nil, func(ctx context.Context, now time.Time) error {
		return a.tunnelPG.ReconcileExpired(ctx, now)
	})
}

func (a *App) startRunReconciler() {
	if a.runReconciler == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			if a.backgroundReconcileContext().Err() != nil {
				return
			}
			a.reconcileComponentWithContext(a.backgroundReconcileContext(), reconcilekernel.ComponentRun, a.now(), func(ctx context.Context, now time.Time) error {
				return a.runReconciler.ReconcilePending(ctx, now)
			})
			waitCtx, cancel := context.WithTimeout(a.backgroundReconcileContext(), a.reconcileInterval)
			_ = a.runReconciler.WaitForWork(waitCtx)
			cancel()
		}
	}()
}

func (a *App) startPeriodicComponent(component string, enabled bool, reconcile func(context.Context, time.Time) error) {
	a.startPeriodicComponentLoop(component, enabled, reconcile, a.reconcileComponent)
}

func (a *App) startPeriodicComponentLoop(component string, enabled bool, reconcile func(context.Context, time.Time) error, run func(string, time.Time, func(context.Context, time.Time) error) error) {
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
				run(component, a.now(), reconcile)
			case <-lifecycleDone:
				return
			case <-a.stopCh:
				return
			}
		}
	}()
}

func (a *App) reconcileV1() {
	a.reconcileComponents(true)
}

func (a *App) reconcilePeriodicV1() {
	a.reconcileComponents(false)
}

func (a *App) reconcileComponents(_ bool) {
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
	if a.tunnelPG != nil {
		a.reconcileComponent(reconcilekernel.ComponentTunnel, a.now(), func(ctx context.Context, now time.Time) error {
			return a.tunnelPG.ReconcileExpired(ctx, now)
		})
	}
}

func (a *App) reconcileComponent(component string, now time.Time, reconcile func(context.Context, time.Time) error) error {
	ctx, cancel := context.WithTimeout(a.backgroundReconcileContext(), a.backgroundReconcileTimeout())
	defer cancel()
	return a.reconcileComponentWithContext(ctx, component, now, reconcile)
}

func (a *App) reconcileComponentWithContext(ctx context.Context, component string, now time.Time, reconcile func(context.Context, time.Time) error) error {
	var run reconcilekernel.RunHandle
	if a.reconcileHealth != nil {
		run = a.reconcileHealth.RecordStart(component, now)
	}
	err := reconcile(ctx, now)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
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
