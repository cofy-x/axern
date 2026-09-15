package controlplane

import (
	"context"
	"testing"
	"time"
)

func TestIdentityRetryBounded(t *testing.T) {
	for failures := 0; failures < 100; failures++ {
		for _, jitter := range []float64{0, 0.5, 0.999999} {
			delay := identityRetryDelay(failures, jitter)
			if delay < 500*time.Millisecond || delay > time.Minute {
				t.Fatalf("retry %d: %v", failures, delay)
			}
		}
	}
	if identityRetryDelay(100, 0) != 30*time.Second {
		t.Fatal("retry cap not reached")
	}
}

func TestIdentityWaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitIdentityDeadline(ctx, time.Now().Add(time.Hour)) {
		t.Fatal("cancelled wait succeeded")
	}
	if waitIdentityDeadline(ctx, time.Now().Add(-time.Hour)) {
		t.Fatal("cancelled overdue wait succeeded")
	}
	if !waitIdentityDeadline(context.Background(), time.Now().Add(-time.Second)) {
		t.Fatal("overdue certificate renewal did not proceed")
	}
}

func TestIdentityMaintenanceCancelledBeforeBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := &NodeEnrollment{BundlePath: t.TempDir() + "/missing"}
	n.Maintain(ctx, "", func(error) { t.Fatal("cancelled maintenance reported failure") })
}

func TestIdentityRenewalDeadlinePreservesValidityMargin(t *testing.T) {
	expires := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, jitter := range []float64{0, 0.5, 0.999999} {
		due := identityRenewalDeadline(expires, jitter)
		remaining := expires.Sub(due)
		if remaining <= 7*time.Hour || remaining > 8*time.Hour {
			t.Fatalf("unsafe renewal margin: %v", remaining)
		}
	}
}
