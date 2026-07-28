package app

import (
	"context"
	"testing"
	"time"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func TestObserveReconcileHealthMetrics(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	app := &App{
		now:             func() time.Time { return now },
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentService),
	}
	run := app.reconcileHealth.RecordStart(reconcilekernel.ComponentService, now.Add(-10*time.Second))
	app.reconcileHealth.RecordResult(run, assertErr("database unavailable"), now.Add(-5*time.Second))

	failures := collectReconcileMetric(t, app.observeReconcileConsecutiveFailures)
	if got := failures[reconcilekernel.ComponentService]; got != 1 {
		t.Fatalf("consecutive failures = %d, want 1", got)
	}
	errorAge := collectReconcileMetric(t, app.observeReconcileLastErrorAge)
	if got := errorAge[reconcilekernel.ComponentService]; got != 5 {
		t.Fatalf("last error age = %d, want 5", got)
	}
	running := collectReconcileMetric(t, app.observeReconcileRunning)
	if got := running[reconcilekernel.ComponentService]; got != 0 {
		t.Fatalf("running = %d, want 0", got)
	}

	run = app.reconcileHealth.RecordStart(reconcilekernel.ComponentService, now.Add(-3*time.Second))
	app.reconcileHealth.RecordResult(run, nil, now.Add(-2*time.Second))
	successAge := collectReconcileMetric(t, app.observeReconcileLastSuccessAge)
	if got := successAge[reconcilekernel.ComponentService]; got != 2 {
		t.Fatalf("last success age = %d, want 2", got)
	}
	failures = collectReconcileMetric(t, app.observeReconcileConsecutiveFailures)
	if got := failures[reconcilekernel.ComponentService]; got != 0 {
		t.Fatalf("consecutive failures after success = %d, want 0", got)
	}
	if got := collectReconcileMetric(t, app.observeReconcileRunningAge)[reconcilekernel.ComponentService]; got != 0 {
		t.Fatalf("idle running age = %d, want 0", got)
	}

	app.reconcileHealth.RecordStart(reconcilekernel.ComponentService, now.Add(-7*time.Second))
	runningAge := collectReconcileMetric(t, app.observeReconcileRunningAge)
	if got := runningAge[reconcilekernel.ComponentService]; got != 7 {
		t.Fatalf("running age = %d, want 7", got)
	}
}

func collectReconcileMetric(t *testing.T, callback sdkobs.Int64GaugeCallback) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	if err := callback(context.Background(), func(value int64, attrs ...attribute.KeyValue) {
		for _, attr := range attrs {
			if string(attr.Key) == "axern.component" {
				out[attr.Value.AsString()] = value
			}
		}
	}); err != nil {
		t.Fatalf("observe metric: %v", err)
	}
	return out
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
