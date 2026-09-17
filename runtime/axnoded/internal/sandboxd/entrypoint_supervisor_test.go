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
	ctx, cancel := context.WithCancel(context.Background())
	runner := NewRunner(cfg, &bytes.Buffer{}, &bytes.Buffer{})
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() { code, err := runner.Run(ctx); done <- result{code, err} }()
	waitForState(t, runner.state, workload.UserStateExited)
	cancel()
	got := <-done
	code, err := got.code, got.err
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
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() { code, err := runner.Run(ctx); done <- result{code, err} }()
	waitForState(t, runner.state, workload.UserStateFailed)
	cancel()
	got := <-done
	code, err := got.code, got.err
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

func TestRunnerCancellationKeepsReaperAliveThroughShutdown(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := NewRunner(Config{
		SocketPath:      filepath.Join(shortTempDir(t), "sandboxd.sock"),
		ShutdownTimeout: time.Second,
		Entrypoint:      workload.Entrypoint{Args: []string{"/bin/sh", "-c", "printf ready; exec sleep 60"}},
	}, writer, writer)
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() { code, err := runner.Run(ctx); done <- result{code, err} }()
	// The child's pipe write establishes startup before cancellation.
	if _, err := reader.Read(make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	cancel()
	got := <-done
	if got.err != nil || got.code != 128+int(syscall.SIGTERM) {
		t.Fatalf("shutdown: code=%d err=%v", got.code, got.err)
	}
	if state := runner.state.Status().UserProcess.State; state != workload.UserStateExited {
		t.Fatalf("unreaped state: %s", state)
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
