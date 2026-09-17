package sandboxd_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	daemon "github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
	"github.com/stretchr/testify/require"
)

func TestStoppedWorkloadKeepsFileServiceAliveUntilSandboxCleanup(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "axd-output-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	socketPath := filepath.Join(root, "sandboxd.sock")
	outputPath := filepath.Join(root, "candidate.txt")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := daemon.NewRunner(daemon.Config{
		SocketPath:      socketPath,
		ShutdownTimeout: time.Second,
		Entrypoint: workload.Entrypoint{Args: []string{
			"/bin/sh", "-c", `printf candidate > "$0"; exec sleep 60`, outputPath,
		}},
	}, &bytes.Buffer{}, &bytes.Buffer{})
	type runResult struct {
		code int
		err  error
	}
	runDone := make(chan runResult, 1)
	go func() {
		code, err := runner.Run(ctx)
		runDone <- runResult{code: code, err: err}
	}()

	client := runtimesandboxd.NewClient(socketPath)
	_, err = client.WaitReady(t.Context(), 2*time.Second, 10*time.Millisecond)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := os.Stat(outputPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)

	stopped, err := client.StopWorkload(t.Context(), "KILL")
	require.NoError(t, err)
	require.Equal(t, 137, stopped.ExitCode)
	waited, err := client.WaitWorkload(t.Context())
	require.NoError(t, err)
	require.Equal(t, stopped.ExitCode, waited.ExitCode)

	contents, err := client.ReadFile(t.Context(), outputPath)
	require.NoError(t, err)
	require.Equal(t, "candidate", string(contents.Data))
	select {
	case result := <-runDone:
		t.Fatalf("sandboxd exited before output sealing: code=%d err=%v", result.code, result.err)
	default:
	}

	cancel()
	result := <-runDone
	require.NoError(t, result.err)
	require.Equal(t, 137, result.code)
}
