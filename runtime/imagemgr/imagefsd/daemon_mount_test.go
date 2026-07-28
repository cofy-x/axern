package imagefsd

import (
	"testing"
)

func TestDaemon_Mount_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateRunning)
	mock.mockIsAlive = func() bool { return true }

	err := mock.Mount()
	if err != nil {
		t.Errorf("Mount() on already running daemon should succeed, got error: %v", err)
	}

	if mock.getState() != DaemonStateRunning {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateRunning)
	}
}

func TestDaemon_Mount_RemountDeadProcess(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateRunning)
	mock.mockIsAlive = func() bool { return false }

	err := mock.Mount()
	if err == nil {
		t.Error("Mount() should fail when trying to start actual process")
	}
	if mock.getState() == DaemonStateRunning {
		t.Error("State should not be Running after failed mount")
	}
}

func TestDaemon_Mount_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.mockIsAlive = func() bool { return false }

	const numGoroutines = 10
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			errChan <- mock.Mount()
		}()
	}

	errors := make([]error, 0, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		errors = append(errors, <-errChan)
	}

	for i, err := range errors {
		if err == nil {
			t.Errorf("Goroutine %d: expected error, got nil", i)
		}
	}
}

func TestDaemon_Mount_StateTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Initial state = %v, want %v", mock.getState(), DaemonStateStopped)
	}

	mock.mockIsAlive = func() bool { return false }
	_ = mock.Mount()

	if mock.getState() == DaemonStateRunning {
		t.Error("State should not be Running after failed mount")
	}
}
