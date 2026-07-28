package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunExecutesRuntimeAndPersistsExitState(t *testing.T) {
	exitStatePath := filepath.Join(t.TempDir(), "exit.json")
	pidFilePath := filepath.Join(t.TempDir(), "runtime.pid")
	stderr := &bytes.Buffer{}

	code := run([]string{
		"--runtime-binary", "/bin/sh",
		"--exit-state", exitStatePath,
		"--pid-file", pidFilePath,
		"--",
		"-c", "exit 7",
	}, stderr)

	assert.Equal(t, 7, code)
	exit, ok, err := readExitState(t, exitStatePath)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 7, exit.ExitCode)
}

func TestRunRejectsMissingRequiredArgs(t *testing.T) {
	stderr := &bytes.Buffer{}

	code := run(nil, stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "runtime-binary is required")
}

func TestRunReportsRuntimeStartFailure(t *testing.T) {
	exitStatePath := filepath.Join(t.TempDir(), "exit.json")
	pidFilePath := filepath.Join(t.TempDir(), "runtime.pid")
	stderr := &bytes.Buffer{}

	code := run([]string{
		"--runtime-binary", filepath.Join(t.TempDir(), "missing-runtime"),
		"--exit-state", exitStatePath,
		"--pid-file", pidFilePath,
		"--",
		"run",
	}, stderr)

	assert.Equal(t, runtimeStartFailureExitCode, code)
	assert.Contains(t, stderr.String(), "run OCI runtime")
	_, err := os.Stat(exitStatePath)
	assert.NoError(t, err)
}

func TestRunMapsSignaledRuntimeToShellExitCode(t *testing.T) {
	exitStatePath := filepath.Join(t.TempDir(), "exit.json")
	pidFilePath := filepath.Join(t.TempDir(), "runtime.pid")
	stderr := &bytes.Buffer{}

	code := run([]string{
		"--runtime-binary", "/bin/sh",
		"--exit-state", exitStatePath,
		"--pid-file", pidFilePath,
		"--",
		"-c", "kill -TERM $$",
	}, stderr)

	wantExitCode := 128 + int(syscall.SIGTERM)
	assert.Equal(t, wantExitCode, code)
	exit, ok, err := readExitState(t, exitStatePath)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, wantExitCode, exit.ExitCode)
}

func readExitState(t *testing.T, path string) (exitState, bool, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return exitState{}, false, nil
		}
		return exitState{}, true, err
	}
	var state exitState
	if err := json.Unmarshal(data, &state); err != nil {
		return exitState{}, true, err
	}
	if state.FinishedAt.IsZero() || time.Since(state.FinishedAt) < 0 {
		t.Fatalf("invalid finished_at: %s", state.FinishedAt)
	}
	return state, true, nil
}
