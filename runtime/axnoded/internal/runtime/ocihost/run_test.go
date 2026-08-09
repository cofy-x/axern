package ocihost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
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

func TestCommonRestartPreservesDurableExitState(t *testing.T) {
	root := t.TempDir()
	common, err := New(Config{Root: root, RuntimeName: "runc", RuntimeBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	want := contract.Exit{Timestamp: time.Now().UTC().Round(time.Second), Status: 13}
	if err := common.PersistExitState("alloc-a", want); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(Config{Root: root, RuntimeName: "runc", RuntimeBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := restarted.ReadExitState("alloc-a", "runc")
	if err != nil || !ok {
		t.Fatalf("ReadExitState() = (%+v, %v, %v)", got, ok, err)
	}
	if got.Status != want.Status || !got.Timestamp.Equal(want.Timestamp) {
		t.Fatalf("exit state = %+v, want %+v", got, want)
	}
}

func TestStartCreateWithExitMonitorPreparesIsolatedState(t *testing.T) {
	root := t.TempDir()
	common, err := New(Config{Root: root, RuntimeName: "runc", RuntimeBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(common.RuntimeExitStatePath("alloc-a"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	var observed InitMonitorStartOptions
	common.SetInitMonitorStarter(func(_ context.Context, options InitMonitorStartOptions) error {
		observed = options
		if _, err := os.Stat(options.ExitStatePath); !os.IsNotExist(err) {
			t.Fatalf("stale exit state was not removed: %v", err)
		}
		return os.WriteFile(options.RuntimePIDPath, []byte("321\n"), 0644)
	})
	if err := common.StartCreateWithExitMonitor(context.Background(), InitMonitorStartOptions{
		ContainerID: "alloc-a",
		RuntimeArgs: []string{"create", "alloc-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if observed.ReadyStatePath != common.InitMonitorReadyStatePath("alloc-a") || observed.RuntimePIDPath != common.RuntimePIDFilePath("alloc-a") {
		t.Fatalf("monitor options = %+v", observed)
	}
}

func TestAwaitInitMonitorExitStateWaitsForDurableResult(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	readyPath := common.InitMonitorReadyStatePath("alloc-a")
	if err := os.MkdirAll(filepath.Dir(readyPath), 0755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(initMonitorReadyState{Ready: true, InitPID: 321, ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyPath, payload, 0644); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := common.AwaitInitMonitorExitState(t.Context(), "alloc-a", "runc")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("AwaitInitMonitorExitState() returned before exit state: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := common.PersistExitState("alloc-a", contract.Exit{Timestamp: time.Now().UTC(), Status: 13}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AwaitInitMonitorExitState() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AwaitInitMonitorExitState() did not observe durable exit state")
	}
}

func TestAwaitInitMonitorExitStateSkipsContainersWithoutMonitorOwnership(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := common.AwaitInitMonitorExitState(t.Context(), "alloc-a", "runc")
	if err != nil || expected {
		t.Fatalf("AwaitInitMonitorExitState() = (%v, %v), want (false, nil)", expected, err)
	}
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
