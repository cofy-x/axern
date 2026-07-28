package imagefsd

import "testing"

func TestDaemon_MountFailure_DoesNotSetMountFailed(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)
	mock.mockIsAlive = func() bool { return false }

	err := mock.Mount()
	if err == nil {
		t.Fatal("Mount() should fail without a real binary")
	}

	if mock.mountFailed.Load() {
		t.Error("mountFailed should NOT be set on immediate start failure (only on timeout)")
	}
}

func TestDaemon_MountFailed_FlagBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mountFailed.Store(true)
	if !mock.mountFailed.Load() {
		t.Fatal("mountFailed should be true after Store(true)")
	}

	mock.setState(DaemonStateRunning)
	mock.mockIsAlive = func() bool { return true }

	err := mock.Mount()
	if err != nil {
		t.Fatalf("Mount() should succeed: %v", err)
	}
	if mock.mountFailed.Load() {
		t.Error("mount() should clear mountFailed at entry")
	}
}

func TestDaemon_Mount_ClearsMountFailed(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mountFailed.Store(true)
	mock.setState(DaemonStateRunning)
	mock.mockIsAlive = func() bool { return true }

	err := mock.Mount()
	if err != nil {
		t.Fatalf("Mount() on running daemon should succeed: %v", err)
	}

	if mock.mountFailed.Load() {
		t.Error("mountFailed should be cleared by a new mount attempt")
	}
}

func TestDaemon_UnmountForGC_ProceedsWhenMountFailed(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mountFailed.Store(true)
	mock.setState(DaemonStateStopped)

	result := mock.unmountForGC()
	if !result {
		t.Error("unmountForGC() should return true when mountFailed is set")
	}
}

func TestDaemon_UnmountForGC_AbortsWhenMountFailedCleared(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mountFailed.Store(false)
	mock.setState(DaemonStateRunning)

	result := mock.unmountForGC()
	if result {
		t.Error("unmountForGC() should return false when mountFailed is not set")
	}
}
