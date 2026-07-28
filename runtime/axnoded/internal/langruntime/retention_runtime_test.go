package langruntime

import (
	"testing"
	"time"
)

func TestSetTemporary_ReleasesIdleRuntimeImmediately(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-idle", "/shared"), false)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	if got := lm.GetLangRuntime("rt-idle"); got == nil {
		t.Fatal("expected runtime to exist before unregister")
	}

	lr.SetTemporary(true)

	if got := lm.GetLangRuntime("rt-idle"); got != nil {
		t.Fatal("expected idle temporary runtime to be cleaned up immediately")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected 1 umount after immediate cleanup, got %d", mock.UmountCount())
	}
}

func TestRetainedRuntimeReuseCancelsEviction(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(200*time.Millisecond, 8)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-reuse", "/reuse"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	lr.IncRef()
	lr.DecRef()
	if !lr.Retained() {
		t.Fatal("expected runtime to enter retained state")
	}

	lr.IncRef()
	if lr.Retained() {
		t.Fatal("expected retained state to clear on reuse")
	}
	if lr.IdleSince() != (time.Time{}) || lr.ExpireAt() != (time.Time{}) {
		t.Fatalf("expected idle/expire timestamps to be reset, got idle=%v expire=%v", lr.IdleSince(), lr.ExpireAt())
	}
	if lr.RootFS.RetainedRefCount() != 0 {
		t.Fatalf("retained rootfs refs = %d, want 0 after reuse", lr.RootFS.RetainedRefCount())
	}

	evictions := lm.collectExpiredRetained(time.Now().UTC().Add(time.Second), RetentionReasonTTLExpired)
	lm.executeEvictions(t.Context(), evictions)

	if got := lm.GetLangRuntime("rt-reuse"); got == nil {
		t.Fatal("expected reused runtime to remain present")
	}
	if mock.UmountCount() != 0 {
		t.Fatalf("expected retained reuse not to umount rootfs, got %d", mock.UmountCount())
	}
}

func TestRetentionDisabledFallsBackToImmediateCleanup(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(0, 0)

	lr, err := addTestLangRuntime(lm, newTestFR("rt-disabled", "/disabled"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}

	lr.IncRef()
	lr.DecRef()

	if got := lm.GetLangRuntime("rt-disabled"); got != nil {
		t.Fatal("expected immediate cleanup when retention is disabled")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("expected rootfs to be umounted immediately when retention is disabled, got %d", mock.UmountCount())
	}
}
