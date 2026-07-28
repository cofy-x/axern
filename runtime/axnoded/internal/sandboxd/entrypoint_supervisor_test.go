package sandboxd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func TestRunnerReturnsUserExitCode(t *testing.T) {
	dir := shortTempDir(t)
	socketPath := filepath.Join(dir, "sandboxd.sock")
	cfg := Config{
		SocketPath:      socketPath,
		ShutdownTimeout: time.Second,
		Entrypoint: workload.Entrypoint{
			Args: []string{"/bin/sh", "-c", "exit 7"},
		},
	}
	code, err := NewRunner(cfg, &bytes.Buffer{}, &bytes.Buffer{}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
}

func TestRunnerReportsStartFailure(t *testing.T) {
	dir := shortTempDir(t)
	socketPath := filepath.Join(dir, "sandboxd.sock")
	cfg := Config{
		SocketPath:      socketPath,
		ShutdownTimeout: time.Second,
		Entrypoint: workload.Entrypoint{
			Args: []string{filepath.Join(dir, "missing-binary")},
		},
	}
	stderr := &bytes.Buffer{}
	runner := NewRunner(cfg, &bytes.Buffer{}, stderr)
	code, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if code != proc.RuntimeStartExitCode {
		t.Fatalf("exit code = %d, want %d", code, proc.RuntimeStartExitCode)
	}
	status := runner.state.Status().UserProcess
	if status.State != workload.UserStateFailed {
		t.Fatalf("state = %q, want %q", status.State, workload.UserStateFailed)
	}
	if status.LastError == "" {
		t.Fatal("last error is empty")
	}
	if !bytes.Contains(stderr.Bytes(), []byte("start user process:")) {
		t.Fatalf("stderr = %q, want start failure diagnostic", stderr.String())
	}
}

func TestSupervisorForwardsSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group signal forwarding is Unix-only")
	}
	dir := shortTempDir(t)
	state := workload.NewState(filepath.Join(dir, "sandboxd.sock"))
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	supervisor := workload.NewSupervisor(workload.Entrypoint{
		Args: []string{"/bin/sh", "-c", "while true; do sleep 1; done"},
	}, 2*time.Second, state, waiter, &bytes.Buffer{}, &bytes.Buffer{})

	done := supervisor.Start()
	waitForState(t, state, workload.UserStateRunning)
	result := supervisor.Shutdown(syscall.SIGTERM)
	<-done
	want := 128 + int(syscall.SIGTERM)
	if result.ExitCode != want {
		t.Fatalf("exit code = %d, want %d", result.ExitCode, want)
	}
	if state.Status().UserProcess.Signal == "" {
		t.Fatal("signal is empty, want forwarded termination signal")
	}
}

func waitForState(t *testing.T, state *workload.State, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state.Status().UserProcess.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q", state.Status().UserProcess.State, want)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "axd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
