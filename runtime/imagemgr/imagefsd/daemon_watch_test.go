package imagefsd

import (
	"bytes"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func TestCheckMountReady_StatfsZeroBlocksStillReady(t *testing.T) {
	prevStatfs := statfsFunc
	t.Cleanup(func() {
		statfsFunc = prevStatfs
	})

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		stat.Bsize = 4096
		stat.Blocks = 0
		return nil
	}
	isReady, fs, err := checkMountReady("/mnt/test")
	if err != nil {
		t.Fatalf("checkMountReady() error = %v, want nil", err)
	}
	if !isReady {
		t.Fatal("checkMountReady() = false, want true")
	}
	if fs.Blocks != 0 {
		t.Fatalf("statfs blocks = %d, want 0", fs.Blocks)
	}
}

func TestCheckMountReady_StatfsNonZeroBlocksWaits(t *testing.T) {
	prevStatfs := statfsFunc
	t.Cleanup(func() {
		statfsFunc = prevStatfs
	})

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		stat.Bsize = 4096
		stat.Blocks = 1
		return nil
	}
	isReady, fs, err := checkMountReady("/mnt/test")
	if err != nil {
		t.Fatalf("checkMountReady() error = %v, want nil", err)
	}
	if isReady {
		t.Fatal("checkMountReady() = true, want false")
	}
	if fs.Blocks != 1 {
		t.Fatalf("statfs blocks = %d, want 1", fs.Blocks)
	}
}

func TestCheckMountReady_StatfsErrorReturnsError(t *testing.T) {
	prevStatfs := statfsFunc
	t.Cleanup(func() {
		statfsFunc = prevStatfs
	})

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		return syscall.EIO
	}
	isReady, _, err := checkMountReady("/mnt/test")
	if err == nil {
		t.Fatal("checkMountReady() error = nil, want non-nil")
	}
	if isReady {
		t.Fatal("checkMountReady() = true, want false")
	}
	if !strings.Contains(err.Error(), "failed to statfs mountpoint") {
		t.Fatalf("error %q does not contain statfs context", err.Error())
	}
}

func TestDaemon_StartDaemonProcess_RequiresStatfsReady(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	prevStatfs := statfsFunc
	t.Cleanup(func() {
		statfsFunc = prevStatfs
	})

	mock.watcherActive.Store(true)
	mock.stopChan = make(chan struct{})
	mock.setState(DaemonStateMounting)

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		stat.Bsize = 4096
		stat.Blocks = 0
		return nil
	}
	mock.startDaemonProcess()

	if mock.getState() != DaemonStateRunning {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateRunning)
	}
}

func TestDaemon_StartDaemonProcess_NonZeroBlocksLogsProgressAndRecovers(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	prevStatfs := statfsFunc
	prevOutput := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	t.Cleanup(func() {
		statfsFunc = prevStatfs
		logrus.SetOutput(prevOutput)
		logrus.SetLevel(prevLevel)
	})

	mock.watcherActive.Store(true)
	mock.stopChan = make(chan struct{})
	mock.setState(DaemonStateMounting)

	var statfsCalls atomic.Int32
	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		if statfsCalls.Add(1) == 1 {
			stat.Bsize = 4096
			stat.Blocks = 1
			return nil
		}
		stat.Bsize = 4096
		stat.Blocks = 0
		return nil
	}
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.InfoLevel)

	mock.startDaemonProcess()

	if mock.getState() != DaemonStateRunning {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateRunning)
	}
	if !strings.Contains(buf.String(), "mount not ready yet, waiting for statfs zero blocks") {
		t.Fatalf("progress log missing, got %q", buf.String())
	}
	if got := statfsCalls.Load(); got < 2 {
		t.Errorf("statfs calls = %d, want at least 2", got)
	}
}

func TestDaemon_StartDaemonProcess_ZeroSizeRuns(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	prevStatfs := statfsFunc
	t.Cleanup(func() {
		statfsFunc = prevStatfs
	})

	mock.watcherActive.Store(true)
	mock.stopChan = make(chan struct{})
	mock.setState(DaemonStateMounting)

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		stat.Bsize = 4096
		stat.Blocks = 0
		return nil
	}
	mock.startDaemonProcess()

	if mock.getState() != DaemonStateRunning {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateRunning)
	}
}

func TestDaemon_StartDaemonProcess_StatfsErrorWarnsAndWaits(t *testing.T) {
	tmpDir := t.TempDir()
	mock := newMockDaemon(tmpDir)

	prevStatfs := statfsFunc
	prevOutput := logrus.StandardLogger().Out
	prevLevel := logrus.GetLevel()
	t.Cleanup(func() {
		statfsFunc = prevStatfs
		logrus.SetOutput(prevOutput)
		logrus.SetLevel(prevLevel)
	})

	mock.watcherActive.Store(true)
	mock.stopChan = make(chan struct{})
	mock.setState(DaemonStateMounting)

	statfsFunc = func(path string, stat *unix.Statfs_t) error {
		return syscall.EIO
	}
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.WarnLevel)

	done := make(chan struct{})
	go func() {
		mock.startDaemonProcess()
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)

	if mock.getState() != DaemonStateMounting {
		t.Errorf("State = %v, want %v", mock.getState(), DaemonStateMounting)
	}
	close(mock.stopChan)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("startDaemonProcess() did not exit after stopChan closed")
	}
	if !strings.Contains(buf.String(), "mount readiness statfs failed, keep waiting") {
		t.Fatalf("warning log missing, got %q", buf.String())
	}

	if mock.getState() != DaemonStateStopped {
		t.Errorf("Final state = %v, want %v", mock.getState(), DaemonStateStopped)
	}
}
