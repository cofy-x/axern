package controlplane

import (
	"context"
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccessGrantCacheWaitValidateWakesForExactToken(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		valid, _ := cache.WaitValidate(ctx, "alloc-1", "token-1", time.Now)
		result <- valid
	}()

	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{
		GrantID:             "grant-1",
		AllocationID:        "alloc-1",
		ValidationTokenHash: accessGrantTokenHash("token-1"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})

	if valid := <-result; !valid {
		t.Fatal("WaitValidate() = false, want true")
	}
}

func TestAccessGrantCacheWaitValidateRejectsKnownRevokedToken(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{
		GrantID:             "grant-1",
		AllocationID:        "alloc-1",
		ValidationTokenHash: accessGrantTokenHash("token-1"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
		Revoked:             true,
	}})

	if valid, _ := cache.WaitValidate(context.Background(), "alloc-1", "token-1", time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestAccessGrantCacheWaitValidateStopsWithContext(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if valid, _ := cache.WaitValidate(ctx, "alloc-1", "unknown-token", time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestAccessGrantCacheApplyPrunesExpiredTokens(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{
		AllocationID:        "alloc-expired",
		ValidationTokenHash: accessGrantTokenHash("expired-token"),
		ExpiresAt:           timestamppb.New(time.Now().Add(-time.Second)),
	}})
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{
		AllocationID:        "alloc-live",
		ValidationTokenHash: accessGrantTokenHash("live-token"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
	}})

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.byToken) != 1 {
		t.Fatalf("cached token count = %d, want 1", len(cache.byToken))
	}
}

func TestAccessGrantCacheApplyReplacesRotatedToken(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	grant := &nodev1.NodeAllocationAccessGrant{
		GrantID:      "grant-1",
		AllocationID: "alloc-1",
		ExpiresAt:    timestamppb.New(time.Now().Add(time.Minute)),
	}
	grant.ValidationTokenHash = accessGrantTokenHash("old-token")
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{grant})
	grant.ValidationTokenHash = accessGrantTokenHash("new-token")
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{grant})

	if cache.Validate("alloc-1", "old-token", time.Now()) {
		t.Fatal("Validate(old-token) = true after rotation")
	}
	if !cache.Validate("alloc-1", "new-token", time.Now()) {
		t.Fatal("Validate(new-token) = false after rotation")
	}
}
