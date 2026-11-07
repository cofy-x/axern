package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
)

type retentionCleanupFunc func(context.Context, time.Time) (retentionkernel.Result, error)

func (f retentionCleanupFunc) Cleanup(ctx context.Context, now time.Time) (retentionkernel.Result, error) {
	return f(ctx, now)
}

func TestRetentionWorkerCloseCancelsAndJoinsCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	a := &App{
		now:             time.Now,
		reconcileCtx:    ctx,
		cancelReconcile: cancel,
		stopCh:          make(chan struct{}),
		retention: retentionCleanupFunc(func(ctx context.Context, _ time.Time) (retentionkernel.Result, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			close(finished)
			return retentionkernel.Result{}, ctx.Err()
		}),
	}
	a.startRetentionWorker(time.Millisecond)
	waitRetentionSignal(t, started)
	closed := make(chan struct{})
	go func() {
		_ = a.Close()
		close(closed)
	}()
	waitRetentionSignal(t, cancelled)
	select {
	case <-closed:
		t.Fatal("Close returned before retention cleanup finished")
	default:
	}
	close(release)
	waitRetentionSignal(t, closed)
	waitRetentionSignal(t, finished)
	if err := a.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

func TestRetentionWorkerRetriesCleanupFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	retried := make(chan struct{})
	var calls atomic.Int32
	a := &App{
		now:             time.Now,
		reconcileCtx:    ctx,
		cancelReconcile: cancel,
		stopCh:          make(chan struct{}),
		retention: retentionCleanupFunc(func(ctx context.Context, _ time.Time) (retentionkernel.Result, error) {
			if calls.Add(1) == 1 {
				return retentionkernel.Result{}, errors.New("temporary database failure")
			}
			close(retried)
			<-ctx.Done()
			return retentionkernel.Result{}, ctx.Err()
		}),
	}
	t.Cleanup(func() { _ = a.Close() })
	a.startRetentionWorker(time.Millisecond)
	waitRetentionSignal(t, retried)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("cleanup calls = %d, want failed attempt and retry", got)
	}
}

func TestRetentionCleanupUsesDeadlineAndApplicationClock(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	wantErr := errors.New("cleanup failed")
	for _, parentTimeout := range []time.Duration{time.Minute, time.Second} {
		t.Run(parentTimeout.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), parentTimeout)
			defer cancel()
			started := time.Now()
			a := &App{
				now: func() time.Time { return now },
				retention: retentionCleanupFunc(func(cleanupCtx context.Context, got time.Time) (retentionkernel.Result, error) {
					if !got.Equal(now) {
						t.Fatalf("cleanup clock = %v, want %v", got, now)
					}
					deadline, ok := cleanupCtx.Deadline()
					if !ok {
						t.Fatal("cleanup has no deadline")
					}
					parentDeadline, _ := ctx.Deadline()
					if parentTimeout < retentionTimeout {
						if !deadline.Equal(parentDeadline) {
							t.Fatalf("cleanup extended parent deadline: %v > %v", deadline, parentDeadline)
						}
					} else if deadline.Before(started.Add(retentionTimeout)) || deadline.After(time.Now().Add(retentionTimeout)) {
						t.Fatalf("cleanup deadline = %v, want 30 seconds from cycle start", deadline)
					}
					return retentionkernel.Result{}, wantErr
				}),
			}
			if err := a.cleanupRetainedHistory(ctx); !errors.Is(err, wantErr) {
				t.Fatalf("cleanup error = %v, want %v", err, wantErr)
			}
		})
	}
}

func TestPostgresRetentionDefaultsAndDisabledPolicy(t *testing.T) {
	a, _ := newPostgresTestService(t)
	if a.retention == nil || a.retentionConfig != retentionkernel.DefaultConfig() {
		t.Fatalf("default retention policy = %+v", a.retentionConfig)
	}
	_ = a.Close()

	cfg := retentionkernel.Config{Enabled: false, Interval: time.Minute, BatchSize: 3}
	a, _ = newPostgresTestServiceWithConfig(t, Config{Retention: &cfg})
	defer a.Close()
	result, err := a.retention.Cleanup(context.Background(), time.Now())
	if err != nil || !result.Skipped {
		t.Fatalf("disabled retention cleanup = %+v, %v", result, err)
	}
	if a.retentionConfig.Interval != time.Minute || a.retentionConfig.BatchSize != 3 {
		t.Fatalf("custom retention policy was lost: %+v", a.retentionConfig)
	}
}

func TestPostgresRetentionWorkerUsesConfiguredPolicy(t *testing.T) {
	cfg := retentionkernel.DefaultConfig()
	cfg.Interval = time.Millisecond
	cfg.QuotaEventsTTL = time.Hour
	a, _ := newPostgresTestServiceWithConfig(t, Config{Retention: &cfg})
	defer a.Close()
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,created_at) VALUES('retention-worker',$1)`, now); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		id      string
		created time.Time
	}{
		{id: "expired", created: now.Add(-2 * time.Hour)},
		{id: "recent", created: now.Add(-30 * time.Minute)},
	} {
		if _, err := a.db.Pool().Exec(ctx, `INSERT INTO namespace_quota_events(event_id,namespace,event_type,environment_id,reason,created_at) VALUES($1,'retention-worker','admission_rejected','retention-environment','insufficient_cpu',$2)`, event.id, event.created); err != nil {
			t.Fatal(err)
		}
	}
	a.startReconciler()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var expiredExists bool
		if err := a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM namespace_quota_events WHERE event_id='expired')`).Scan(&expiredExists); err != nil {
			t.Fatal(err)
		}
		if !expiredExists {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("configured retention worker did not delete expired quota history")
		case <-ticker.C:
		}
	}
	var recentExists bool
	if err := a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM namespace_quota_events WHERE event_id='recent')`).Scan(&recentExists); err != nil {
		t.Fatal(err)
	}
	if !recentExists {
		t.Fatal("retention worker deleted history inside configured TTL")
	}
}

func waitRetentionSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("retention worker did not reach expected lifecycle boundary")
	}
}
