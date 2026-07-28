package imagefsd

import "testing"

func TestDaemon_RemountAfterFailure_ClearsFlag(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mountFailed.Store(true)
	mock.setState(DaemonStateRunning)
	mock.mockIsAlive = func() bool { return true }

	err := mock.Mount()
	if err != nil {
		t.Fatalf("Mount() should succeed: %v", err)
	}
	if mock.mountFailed.Load() {
		t.Error("mountFailed should be false after successful remount")
	}
}
