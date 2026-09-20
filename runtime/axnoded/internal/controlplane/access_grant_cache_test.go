package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type deadlineCheckingNodeControlProvider struct{ sawDeadline bool }

func (p *deadlineCheckingNodeControlProvider) Client(ctx context.Context) (nodev1.NodeControlClient, error) {
	deadline, ok := ctx.Deadline()
	p.sawDeadline = ok && time.Until(deadline) > 0 && time.Until(deadline) <= accessGrantWatchConnectTimeout
	return nil, errors.New("dial unavailable")
}

func (*deadlineCheckingNodeControlProvider) Close() error { return nil }

func TestAccessGrantWatcherBoundsConnectionEstablishment(t *testing.T) {
	provider := &deadlineCheckingNodeControlProvider{}
	watcher := NewAccessGrantWatcher(
		WithAccessGrantWatcherTarget("controld:24000"),
		WithAccessGrantWatcherNode("node-a"),
		WithAccessGrantWatcherControl(provider),
		WithAccessGrantWatcherCache(NewAccessGrantCache()),
	)
	if _, err := watcher.watchOnce(0); err == nil {
		t.Fatal("watchOnce() error = nil, want dial failure")
	}
	if !provider.sawDeadline {
		t.Fatal("watch connection establishment did not receive a bounded context")
	}
	watcher.Stop()
}

func TestAccessGrantCacheWaitValidateWakesForExactToken(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan bool, 1)
	go func() {
		valid, _ := cache.WaitValidate(ctx, "alloc-1", "token-1", gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, time.Now)
		result <- valid
	}()

	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
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
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
		GrantID:             "grant-1",
		AllocationID:        "alloc-1",
		ValidationTokenHash: accessGrantTokenHash("token-1"),
		ExpiresAt:           timestamppb.New(time.Now().Add(time.Minute)),
		Revoked:             true,
	}})

	if valid, _ := cache.WaitValidate(context.Background(), "alloc-1", "token-1", gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestAccessGrantCacheWaitValidateStopsWithContext(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if valid, _ := cache.WaitValidate(ctx, "alloc-1", "unknown-token", gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, time.Now); valid {
		t.Fatal("WaitValidate() = true, want false")
	}
}

func TestAccessGrantCacheApplyPrunesExpiredTokens(t *testing.T) {
	t.Parallel()

	cache := NewAccessGrantCache()
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
		AllocationID:        "alloc-expired",
		ValidationTokenHash: accessGrantTokenHash("expired-token"),
		ExpiresAt:           timestamppb.New(time.Now().Add(-time.Second)),
	}})
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
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
	grant := &nodev1.NodeAllocationAccessGrant{Revision: 1, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
		GrantID:      "grant-1",
		AllocationID: "alloc-1",
		ExpiresAt:    timestamppb.New(time.Now().Add(time.Minute)),
	}
	grant.ValidationTokenHash = accessGrantTokenHash("old-token")
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{grant})
	grant.Revision = 2
	grant.ValidationTokenHash = accessGrantTokenHash("new-token")
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{grant})

	if cache.Validate("alloc-1", "old-token", time.Now()) {
		t.Fatal("Validate(old-token) = true after rotation")
	}
	if !cache.Validate("alloc-1", "new-token", time.Now()) {
		t.Fatal("Validate(new-token) = false after rotation")
	}
}

func TestAccessGrantRejectsStaleRevocationAndWrongPurpose(t *testing.T) {
	cache := NewAccessGrantCache()
	g := &nodev1.NodeAllocationAccessGrant{GrantID: "g", AllocationID: "a", ValidationTokenHash: accessGrantTokenHash("token"), Revision: 2, Revoked: true, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{g})
	g.Revision = 1
	g.Revoked = false
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{g})
	if valid, _ := cache.WaitValidate(context.Background(), "a", "token", gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, time.Now); valid {
		t.Fatal("old update resurrected revoked token")
	}
	g.GrantID = "other"
	g.ValidationTokenHash = accessGrantTokenHash("output-only")
	g.Revision = 3
	cache.Apply([]*nodev1.NodeAllocationAccessGrant{g})
	if cache.Validate("a", "output-only", time.Now()) {
		t.Fatal("output token authorized interactive access")
	}
	if valid, _ := cache.WaitValidate(context.Background(), "a", "output-only", gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, time.Now); !valid {
		t.Fatal("output token rejected")
	}
}
