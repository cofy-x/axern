package environmentcache

import (
	"testing"
	"time"
)

func TestRetainedEnvironmentReuseCancelsEviction(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(200*time.Millisecond, 8)

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-reuse", "/reuse"))
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}

	environment.IncRef()
	environment.DecRef()
	if !environment.Retained() {
		t.Fatal("expected runtime to enter retained state")
	}

	environment.IncRef()
	if environment.Retained() {
		t.Fatal("expected retained state to clear on reuse")
	}
	if environment.IdleSince() != (time.Time{}) || environment.ExpireAt() != (time.Time{}) {
		t.Fatalf("expected idle/expire timestamps to be reset, got idle=%v expire=%v", environment.IdleSince(), environment.ExpireAt())
	}
	if environment.RootFS.RetainedRefCount() != 0 {
		t.Fatalf("retained rootfs refs = %d, want 0 after reuse", environment.RootFS.RetainedRefCount())
	}

	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(time.Second), RetentionReasonTTLExpired)
	lm.executeEvictions(evictions)

	if got := lm.GetPreparedEnvironment("rt-reuse"); got == nil {
		t.Fatal("expected reused runtime to remain present")
	}
	if mock.UmountCount() != 0 {
		t.Fatalf("expected retained reuse not to umount rootfs, got %d", mock.UmountCount())
	}
}

func TestRetentionDisabledFallsBackToImmediateCleanup(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(0, 0)

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-disabled", "/disabled"))
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}

	environment.IncRef()
	environment.DecRef()

	if got := lm.GetPreparedEnvironment("rt-disabled"); got != nil {
		t.Fatal("expected immediate cleanup when retention is disabled")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected rootfs to be umounted immediately when retention is disabled, got %d", mock.UmountCount())
	}
}
