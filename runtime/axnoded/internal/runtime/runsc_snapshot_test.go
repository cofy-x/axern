package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"github.com/stretchr/testify/assert"
)

func TestRootfsSnapshotUsesDedicatedInheritedFileDescriptor(t *testing.T) {
	assert.Equal(t, []string{"tar", "rootfs-upper", "--file=/proc/self/fd/3", "allocation-one"}, rootfsSnapshotArgs("allocation-one"))
	handler := newSnapshotCommandTestHandler(t)
	var destination bytes.Buffer
	if err := handler.SnapshotRootfsUpper(context.Background(), "success", &destination); err != nil {
		t.Fatal(err)
	}
	if got := destination.String(); got != "pure-tar" {
		t.Fatalf("snapshot destination = %q, want pure tar bytes only", got)
	}
}

func TestRootfsSnapshotReportsCommandStartFailure(t *testing.T) {
	common, err := ocihost.New(ocihost.Config{Root: t.TempDir(), RuntimeBinary: filepath.Join(t.TempDir(), "missing-runsc")})
	if err != nil {
		t.Fatal(err)
	}
	err = (&RunscServiceHandler{common: common}).SnapshotRootfsUpper(context.Background(), "success", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "start runsc rootfs upper layer export") {
		t.Fatalf("SnapshotRootfsUpper() error = %v", err)
	}
}

func TestRootfsSnapshotReportsRunscExportFailure(t *testing.T) {
	err := newSnapshotCommandTestHandler(t).SnapshotRootfsUpper(context.Background(), "export-fail", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "runsc export failed") {
		t.Fatalf("SnapshotRootfsUpper() error = %v", err)
	}
}

func TestRootfsSnapshotPreservesDestinationFailure(t *testing.T) {
	want := errors.New("destination unavailable")
	err := newSnapshotCommandTestHandler(t).SnapshotRootfsUpper(context.Background(), "large", errorWriter{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("SnapshotRootfsUpper() error = %v, want destination failure", err)
	}
}

func TestRootfsSnapshotCancellationTerminatesCommandAndCopy(t *testing.T) {
	handler := newSnapshotCommandTestHandler(t)
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- handler.SnapshotRootfsUpper(ctx, "block-"+ready, &bytes.Buffer{})
	}()
	waitForSnapshotHelperFile(t, ready)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("SnapshotRootfsUpper() error = %v, want context canceled", err)
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func newSnapshotCommandTestHandler(t *testing.T) *RunscServiceHandler {
	t.Helper()
	script := filepath.Join(t.TempDir(), "runsc-test")
	content := `#!/usr/bin/env bash
set -eu
mode="${!#}"
case "${mode}" in
  success)
    printf 'diagnostic on stdout\n'
    printf 'pure-tar' >&3
    ;;
  export-fail)
    printf 'runsc export failed\n'
    exit 23
    ;;
  large)
    while :; do printf '0123456789abcdef' >&3; done
    ;;
  block-*)
    : > "${mode#block-}"
    while :; do :; done
    ;;
  *)
    printf 'unexpected mode %s\n' "${mode}" >&2
    exit 64
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	common, err := ocihost.New(ocihost.Config{Root: t.TempDir(), RuntimeBinary: script})
	if err != nil {
		t.Fatal(err)
	}
	return &RunscServiceHandler{common: common}
}

func waitForSnapshotHelperFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal(fmt.Errorf("snapshot helper did not start before deadline"))
		}
		runtime.Gosched()
	}
}
