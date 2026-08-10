//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInitMonitorPersistsReapedOCIInitExit(t *testing.T) {
	t.Cleanup(func() {
		_ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0)
	})
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "runtime.pid")
	exitPath := filepath.Join(dir, "exit.json")
	readyPath := filepath.Join(dir, "ready.json")
	var stderr bytes.Buffer

	code := runInitMonitor(initMonitorConfig{
		runtimeBinary: "/bin/sh",
		runtimeArgs: []string{
			"-c",
			`(sleep 0.1; exit 13) & child=$!; printf '%s\n' "$child" > "$1"`,
			"sh",
			pidPath,
		},
		exitStatePath:  exitPath,
		pidFilePath:    pidPath,
		readyStatePath: readyPath,
	}, &stderr)
	if code != 0 {
		t.Fatalf("runInitMonitor() code = %d, stderr = %q", code, stderr.String())
	}

	readyPayload, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := decodeMonitorReadyState(readyPayload)
	if err != nil {
		t.Fatal(err)
	}
	if !ready.Ready || ready.InitPID <= 0 {
		t.Fatalf("ready state = %+v", ready)
	}

	exitPayload, err := os.ReadFile(exitPath)
	if err != nil {
		t.Fatal(err)
	}
	var exit exitState
	if err := json.Unmarshal(exitPayload, &exit); err != nil {
		t.Fatal(err)
	}
	if exit.ExitCode != 13 || exit.FinishedAt.IsZero() {
		t.Fatalf("exit state = %+v, want exit code 13", exit)
	}
}
