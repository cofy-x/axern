package controlplane

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLeaseCacheWaitValidateWakesForExactToken(t *testing.T) {
	t.Parallel()

	cache := NewLeaseCache()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		valid, _ := cache.WaitValidate(ctx, "alloc-1", 1, "token-1", time.Now)
		result <- valid
	}()

	cache.Apply([]*commonv1.ExecutionLease{{
		LeaseID:             "lease-1",
		AllocationID:        "alloc-1",
		Attempt:             1,
		ValidationTokenHash: leaseTokenHash("token-1"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})

	if valid := <-result; !valid {
		t.Fatal("WaitValidate() = false, want true")
	}
}

func TestLeaseCacheWaitValidateRejectsKnownRevokedToken(t *testing.T) {
	t.Parallel()

	cache := NewLeaseCache()
	cache.Apply([]*commonv1.ExecutionLease{{
		LeaseID:             "lease-1",
		AllocationID:        "alloc-1",
		Attempt:             1,
		ValidationTokenHash: leaseTokenHash("token-1"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
		Revoked:             true,
	}})

	if valid, _ := cache.WaitValidate(context.Background(), "alloc-1", 1, "token-1", time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestLeaseCacheWaitValidateStopsWithContext(t *testing.T) {
	t.Parallel()

	cache := NewLeaseCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if valid, _ := cache.WaitValidate(ctx, "alloc-1", 1, "unknown-token", time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestLeaseCacheApplyPrunesExpiredTokens(t *testing.T) {
	t.Parallel()

	cache := NewLeaseCache()
	cache.Apply([]*commonv1.ExecutionLease{{
		AllocationID:        "alloc-expired",
		Attempt:             1,
		ValidationTokenHash: leaseTokenHash("expired-token"),
		ExpiresAt:           timestamppb.New(time.Now().Add(-time.Second)),
	}})
	cache.Apply([]*commonv1.ExecutionLease{{
		AllocationID:        "alloc-live",
		Attempt:             1,
		ValidationTokenHash: leaseTokenHash("live-token"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.byToken) != 1 {
		t.Fatalf("cached token count = %d, want 1", len(cache.byToken))
	}
}

func TestLeaseCacheApplyReplacesRotatedToken(t *testing.T) {
	t.Parallel()

	cache := NewLeaseCache()
	lease := &commonv1.ExecutionLease{
		LeaseID:      "lease-1",
		AllocationID: "alloc-1",
		Attempt:      1,
		ExpiresAt:    timestamppb.New(time.Now().Add(time.Minute)),
	}
	lease.ValidationTokenHash = leaseTokenHash("old-token")
	cache.Apply([]*commonv1.ExecutionLease{lease})
	lease.ValidationTokenHash = leaseTokenHash("new-token")
	cache.Apply([]*commonv1.ExecutionLease{lease})

	if cache.Validate("alloc-1", 1, "old-token", time.Now()) {
		t.Fatal("Validate(old-token) = true after rotation")
	}
	if !cache.Validate("alloc-1", 1, "new-token", time.Now()) {
		t.Fatal("Validate(new-token) = false after rotation")
	}
}
