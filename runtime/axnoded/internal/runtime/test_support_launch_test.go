package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func newLocalCreateRequest(t *testing.T) *apipb.CreateContainerRequest {
	t.Helper()

	return &apipb.CreateContainerRequest{
		Runtime: config.RuntimeNameRunsc,
		Rootfs: &apipb.Rootfs{
			Type:     "local",
			RootDir:  newReadonlyRootfs(t),
			Readonly: true,
		},
		Command: []string{"/bin/sh", "-c", "exit 0"},
	}
}

func newReadonlyRootfs(t *testing.T) string {
	t.Helper()

	rootfsDir := t.TempDir()
	for _, dir := range []string{"dev", "mnt", "proc", "sys", "tmp"} {
		if err := os.MkdirAll(filepath.Join(rootfsDir, dir), 0755); err != nil {
			t.Fatalf("mkdir rootfs %s: %v", dir, err)
		}
	}
	return rootfsDir
}

func writeFakeOCIRuntimeBinary(t *testing.T, rootDir, runtimeName string) string {
	t.Helper()

	writeFakeSandboxdBinary(t, rootDir)

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binPath := filepath.Join(binDir, runtimeName)
	script := `#!/bin/sh
pid_file=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--pid-file" ]; then
    shift
    pid_file="$1"
  fi
  shift
done
if [ -n "$pid_file" ]; then
  mkdir -p "$(dirname "$pid_file")" || exit 1
  echo $$ > "$pid_file"
  sleep 0.2
fi
exit 0
`
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake runtime binary: %v", err)
	}
	return binPath
}

func writeFakeSandboxdBinary(t *testing.T, rootDir string) string {
	t.Helper()

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binPath := filepath.Join(binDir, "axern-sandboxd")
	script := `#!/bin/sh
exit 0
`
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake sandboxd binary: %v", err)
	}
	t.Setenv("AXERN_SANDBOXD_BINARY", binPath)
	return binPath
}

func writeFakeRuntimeRunnerBinary(t *testing.T, rootDir string) string {
	t.Helper()

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binPath := filepath.Join(binDir, "axnoded-runtime-runner")
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
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake runtime runner binary: %v", err)
	}
	return binPath
}

func disableSandboxReadyWait(t *testing.T, handler any) {
	t.Helper()

	waiter := func(context.Context, string, *apipb.ContainerMetadata) error { return nil }
	switch h := handler.(type) {
	case *RuncServiceHandler:
		h.waitForSandboxReady = waiter
	case *RunscServiceHandler:
		h.waitForSandboxReady = waiter
	default:
		t.Fatalf("unsupported handler type %T", handler)
	}
}

func waitForPersistedExitState(t *testing.T, read func() (contract.Exit, bool, error)) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, ok, err := read()
		if err != nil {
			t.Fatalf("read persisted exit state: %v", err)
		}
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for persisted exit state")
}
