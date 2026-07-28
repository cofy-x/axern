package ocihost

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStartRunWithExitStateAttachesIOAndPersistsExitState(t *testing.T) {
	runnerPath := writeRuntimeRunnerTestScript(t)
	common, err := New(Config{
		Root:                t.TempDir(),
		RuntimeName:         "testrt",
		RuntimeBinary:       "/bin/sh",
		RuntimeRunnerBinary: runnerPath,
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")

	waitCh, err := common.StartRunWithExitState(stdoutPath, stderrPath, "axctl-test", []string{
		"-c", "echo stdout-line; echo stderr-line >&2; exit 7",
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	waitErr := <-waitCh
	assert.Error(t, waitErr)

	stdoutData, err := os.ReadFile(stdoutPath)
	assert.NoError(t, err)
	assert.Contains(t, string(stdoutData), "stdout-line")

	stderrData, err := os.ReadFile(stderrPath)
	assert.NoError(t, err)
	assert.Contains(t, string(stderrData), "stderr-line")

	exit, ok, err := common.ReadExitState("axctl-test", "testrt")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 7, exit.Status)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), exit.Timestamp)
}

func TestStartRunWithExitStateRequiresRunnerBinary(t *testing.T) {
	common, err := New(Config{
		Root:          t.TempDir(),
		RuntimeName:   "testrt",
		RuntimeBinary: "/bin/sh",
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}

	_, err = common.StartRunWithExitState(
		filepath.Join(t.TempDir(), "stdout.log"),
		filepath.Join(t.TempDir(), "stderr.log"),
		"axctl-test",
		nil,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "runtime runner binary is required")
}

func writeRuntimeRunnerTestScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-runner")
	script := `#!/bin/sh
runtime=""
exit_state=""
pid_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --runtime-binary) runtime="$2"; shift 2 ;;
    --exit-state) exit_state="$2"; shift 2 ;;
    --pid-file) pid_file="$2"; shift 2 ;;
    --) shift; break ;;
    *) echo "unexpected arg: $1" >&2; exit 2 ;;
  esac
done
mkdir -p "$(dirname "$exit_state")" "$(dirname "$pid_file")"
"$runtime" "$@"
code=$?
printf '{"exitCode":%s,"finishedAt":"2024-01-01T00:00:00Z"}\n' "$code" > "$exit_state"
exit "$code"
`
	assert.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return path
}
