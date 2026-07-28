package app

import (
	"context"
	"time"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func (a *App) observeReconcileConsecutiveFailures(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, component := range a.reconcileHealthSnapshot().Components {
		observe(component.ConsecutiveFailures, reconcileComponentAttr(component.Component))
	}
	return nil
}

func (a *App) observeReconcileLastSuccessAge(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	now := a.now()
	for _, component := range a.reconcileHealthSnapshot().Components {
		if component.LastSuccessAt == nil {
			continue
		}
		observe(nonNegativeAgeSeconds(now, *component.LastSuccessAt), reconcileComponentAttr(component.Component))
	}
	return nil
}

func (a *App) observeReconcileLastErrorAge(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	now := a.now()
	for _, component := range a.reconcileHealthSnapshot().Components {
		if component.LastErrorAt == nil {
			continue
		}
		observe(nonNegativeAgeSeconds(now, *component.LastErrorAt), reconcileComponentAttr(component.Component))
	}
	return nil
}

func (a *App) observeReconcileRunning(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, component := range a.reconcileHealthSnapshot().Components {
		if component.Running {
			observe(1, reconcileComponentAttr(component.Component))
			continue
		}
		observe(0, reconcileComponentAttr(component.Component))
	}
	return nil
}

func (a *App) observeReconcileRunningAge(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	now := a.now()
	for _, component := range a.reconcileHealthSnapshot().Components {
		if !component.Running {
			observe(0, reconcileComponentAttr(component.Component))
			continue
		}
		runningSince := component.RunningSince
		if runningSince == nil {
			runningSince = component.LastStartedAt
		}
		if runningSince != nil {
			observe(nonNegativeAgeSeconds(now, *runningSince), reconcileComponentAttr(component.Component))
		}
	}
	return nil
}

func (a *App) reconcileHealthSnapshot() reconcilekernel.HealthSnapshot {
	if a == nil || a.reconcileHealth == nil {
		return reconcilekernel.EmptyHealthSnapshot()
	}
	return a.reconcileHealth.Snapshot()
}

func reconcileComponentAttr(component string) attribute.KeyValue {
	return attribute.String(sdkobs.AttrComponent, component)
}

func nonNegativeAgeSeconds(now time.Time, since time.Time) int64 {
	age := now.Sub(since)
	if age < 0 {
		return 0
	}
	return int64(age.Seconds())
}
