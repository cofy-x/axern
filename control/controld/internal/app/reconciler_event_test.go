package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
)

func TestServiceReconcileNotificationWakesBackgroundLoop(t *testing.T) {
	called := make(chan string, 2)
	app := &App{
		reconcileInterval:       time.Hour,
		now:                     func() time.Time { return time.Now().UTC() },
		stopCh:                  make(chan struct{}),
		pendingServiceReconcile: newServiceReconcileQueue(),
		serviceReconciler: reconcileFunc{
			reconcileServices: func(_ context.Context, serviceIDs []string, _ time.Time) error {
				called <- serviceIDs[0]
				return nil
			},
		},
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentService),
	}
	app.startReconciler()
	t.Cleanup(func() { _ = app.Close() })

	app.notifyServiceReconcile("svc-b", "svc-a")
	got := make(map[string]bool, 2)
	for range 2 {
		select {
		case serviceID := <-called:
			got[serviceID] = true
		case <-time.After(time.Second):
			t.Fatal("service reconcile notification did not wake background loop")
		}
	}
	if len(got) != 2 || !got["svc-a"] || !got["svc-b"] {
		t.Fatalf("reconciled service IDs = %#v, want svc-a and svc-b", got)
	}
}

func TestServiceReconcileNotificationsCoalesce(t *testing.T) {
	app := &App{pendingServiceReconcile: newServiceReconcileQueue()}
	app.notifyServiceReconcile("svc-a")
	app.notifyServiceReconcile("svc-a", "svc-b")
	if got := len(app.pendingServiceReconcile.wake); got != 1 {
		t.Fatalf("queued service reconcile notifications = %d, want 1", got)
	}
	first := app.pendingServiceReconcile.Take()
	second := app.pendingServiceReconcile.Take()
	got := map[string]bool{first.ServiceID: true, second.ServiceID: true}
	if len(got) != 2 || !got["svc-a"] || !got["svc-b"] {
		t.Fatalf("queued service IDs = %#v, want svc-a and svc-b", got)
	}
}

func TestServiceReconcileEventsDoNotBlockIndependentBatches(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	app := &App{
		reconcileInterval:       time.Hour,
		serviceReconcileWorkers: 2,
		now:                     func() time.Time { return time.Now().UTC() },
		stopCh:                  make(chan struct{}),
		pendingServiceReconcile: newServiceReconcileQueue(),
		serviceReconciler: reconcileFunc{
			reconcileServices: func(_ context.Context, serviceIDs []string, _ time.Time) error {
				switch serviceIDs[0] {
				case "svc-a":
					close(firstStarted)
					<-releaseFirst
				case "svc-b":
					close(secondStarted)
				}
				return nil
			},
		},
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentService),
	}
	app.startReconciler()
	t.Cleanup(func() {
		close(releaseFirst)
		_ = app.Close()
	})

	app.notifyServiceReconcile("svc-a")
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first service reconcile did not start")
	}
	app.notifyServiceReconcile("svc-b")
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("independent service reconcile was blocked by the active batch")
	}
}

func TestPeriodicReconcileSeparatesAutoscalingFromRecoverySweep(t *testing.T) {
	pendingCalls := 0
	autoscalingCalls := 0
	app := &App{
		now: func() time.Time { return time.Now().UTC() },
		serviceReconciler: reconcileFunc{
			reconcilePending: func(context.Context, time.Time) error {
				pendingCalls++
				return nil
			},
			reconcileAutoscaled: func(context.Context, time.Time) error {
				autoscalingCalls++
				return nil
			},
		},
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentService),
	}

	app.reconcilePeriodicV1()
	if pendingCalls != 0 || autoscalingCalls != 1 {
		t.Fatalf("periodic calls = pending:%d autoscaling:%d, want 0/1", pendingCalls, autoscalingCalls)
	}
	app.reconcileV1()
	if pendingCalls != 1 || autoscalingCalls != 1 {
		t.Fatalf("recovery calls = pending:%d autoscaling:%d, want 1/1", pendingCalls, autoscalingCalls)
	}
}

func TestPeriodicReconcileComponentsAreIsolated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runStarted := make(chan struct{}, 1)
	nodeCalled := make(chan struct{}, 1)
	app := &App{
		reconcileInterval: 5 * time.Millisecond,
		reconcileTimeout:  time.Second,
		now:               func() time.Time { return time.Now().UTC() },
		reconcileCtx:      ctx,
		cancelReconcile:   cancel,
		stopCh:            make(chan struct{}),
		runReconciler: runReconcilerFunc(func(ctx context.Context, _ time.Time) error {
			select {
			case runStarted <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		}),
		nodeReconciler: availabilityReconcilerFunc(func(context.Context, time.Time) error {
			select {
			case nodeCalled <- struct{}{}:
			default:
			}
			return nil
		}),
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentRun, reconcilekernel.ComponentNode),
	}
	app.startPeriodicReconciler()
	select {
	case <-runStarted:
	case <-time.After(time.Second):
		t.Fatal("run reconcile did not start")
	}
	select {
	case <-nodeCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("node reconcile was blocked by independent run reconcile")
	}
	done := make(chan struct{})
	go func() {
		_ = app.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("App.Close did not cancel active periodic reconcile")
	}
}

func TestPeriodicReconcileStopsWhenLifecycleIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{}, 1)
	app := &App{
		reconcileInterval: 5 * time.Millisecond,
		reconcileTimeout:  time.Second,
		now:               func() time.Time { return time.Now().UTC() },
		reconcileCtx:      ctx,
		cancelReconcile:   cancel,
		stopCh:            make(chan struct{}),
		runReconciler: runReconcilerFunc(func(context.Context, time.Time) error {
			select {
			case called <- struct{}{}:
			default:
			}
			return nil
		}),
		reconcileHealth: reconcilekernel.NewHealthTracker(reconcilekernel.ComponentRun),
	}
	app.startPeriodicReconciler()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("run reconcile did not start")
	}

	cancel()
	done := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("periodic reconcile did not stop after lifecycle cancellation")
	}
	_ = app.Close()
}

func TestReconcileComponentTimeoutIsRecordedAndCloseCancelsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		reconcileTimeout: 20 * time.Millisecond,
		now:              func() time.Time { return time.Now().UTC() },
		reconcileCtx:     ctx,
		cancelReconcile:  cancel,
		stopCh:           make(chan struct{}),
		reconcileHealth:  reconcilekernel.NewHealthTracker(reconcilekernel.ComponentRun),
	}
	err := app.reconcileComponent(reconcilekernel.ComponentRun, app.now(), func(ctx context.Context, _ time.Time) error {
		<-ctx.Done()
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reconcile error = %v, want deadline exceeded", err)
	}
	got := app.reconcileHealth.Snapshot().Components[0]
	if got.Running || got.ConsecutiveFailures != 1 || !strings.Contains(got.LastError, context.DeadlineExceeded.Error()) {
		t.Fatalf("health = %#v, want terminal timeout failure", got)
	}
	done := make(chan struct{})
	go func() {
		_ = app.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("App.Close did not complete after reconcile cancellation")
	}
}

type reconcileFunc struct {
	reconcilePending    func(context.Context, time.Time) error
	reconcileAutoscaled func(context.Context, time.Time) error
	reconcileServices   func(context.Context, []string, time.Time) error
}

type runReconcilerFunc func(context.Context, time.Time) error

func (f runReconcilerFunc) ReconcilePending(ctx context.Context, now time.Time) error {
	return f(ctx, now)
}

type availabilityReconcilerFunc func(context.Context, time.Time) error

func (f availabilityReconcilerFunc) ReconcileUnavailableNodes(ctx context.Context, now time.Time) error {
	return f(ctx, now)
}

func (f reconcileFunc) ReconcilePending(ctx context.Context, now time.Time) error {
	if f.reconcilePending == nil {
		return nil
	}
	return f.reconcilePending(ctx, now)
}

func (f reconcileFunc) ReconcileAutoscaled(ctx context.Context, now time.Time) error {
	if f.reconcileAutoscaled == nil {
		return nil
	}
	return f.reconcileAutoscaled(ctx, now)
}

func (f reconcileFunc) ReconcileServices(ctx context.Context, serviceIDs []string, now time.Time) error {
	if f.reconcileServices == nil {
		return nil
	}
	return f.reconcileServices(ctx, serviceIDs, now)
}
