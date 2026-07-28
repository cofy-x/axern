package imagefsd

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestDaemon_Unmount_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateStopped)

	err := mock.Unmount()
	if err != nil {
		t.Errorf("Unmount() on stopped daemon returned error: %v", err)
	}

	if mock.getState() != DaemonStateStopped {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateStopped)
	}
}

func TestDaemon_Unmount_AlreadyUnmounting(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateUnmounting)

	err := mock.Unmount()
	if err != nil {
		t.Errorf("Unmount() returned error: %v", err)
	}
}

func TestDaemon_Unmount_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateRunning)
	stopChan := make(chan struct{})
	mock.stopChan = stopChan
	mock.kickStop = NewStopper()
	mock.mockIsAlive = func() bool { return false }

	go func() {
		<-mock.kickStop.Done()
		close(stopChan)
	}()

	const numGoroutines = 5
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			errChan <- mock.Unmount()
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		err := <-errChan
		if err != nil {
			t.Errorf("Unmount() call %d failed: %v", i, err)
		}
	}

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Final state = %v, want %v", mock.getState(), DaemonStateStopped)
	}
}

func TestDaemon_Unmount_StateTransition(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateRunning)
	mock.stopChan = make(chan struct{})
	mock.kickStop = NewStopper()
	mock.mockIsAlive = func() bool { return false }

	go func() {
		<-mock.kickStop.Done()
		close(mock.stopChan)
	}()

	err := mock.Unmount()
	if err != nil {
		t.Errorf("Unmount() failed: %v", err)
	}

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Final state = %v, want %v", mock.getState(), DaemonStateStopped)
	}
}

func TestDaemon_Mount_Unmount_Sequence(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Initial state = %v, want %v", mock.getState(), DaemonStateStopped)
	}

	err := mock.Unmount()
	if err != nil {
		t.Errorf("Unmount() on stopped daemon failed: %v", err)
	}

	mock.setState(DaemonStateRunning)
	mock.stopChan = make(chan struct{})
	mock.kickStop = NewStopper()
	mock.mockIsAlive = func() bool { return true }

	err = mock.Mount()
	if err != nil {
		t.Errorf("Mount() on running daemon failed: %v", err)
	}

	fakePid := 99999
	err = os.WriteFile(mock.meta.PidFilePath, []byte(fmt.Sprintf("%d\n", fakePid)), 0644)
	if err != nil {
		t.Fatalf("Failed to create PID file: %v", err)
	}

	mock.mockIsAlive = func() bool { return false }

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(mock.stopChan)
	}()

	err = mock.Unmount()
	if err != nil {
		t.Errorf("Unmount() failed: %v", err)
	}

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Final state = %v, want %v", mock.getState(), DaemonStateStopped)
	}
}

func TestDaemon_CompareAndSwapState(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	mock.setState(DaemonStateStopped)

	if !mock.compareAndSwapState(DaemonStateStopped, DaemonStateMounting) {
		t.Error("compareAndSwapState should succeed")
	}
	if mock.getState() != DaemonStateMounting {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateMounting)
	}

	if mock.compareAndSwapState(DaemonStateStopped, DaemonStateRunning) {
		t.Error("compareAndSwapState should fail with wrong old value")
	}
	if mock.getState() != DaemonStateMounting {
		t.Errorf("State = %v, want %v (should be unchanged)", mock.getState(), DaemonStateMounting)
	}
}
