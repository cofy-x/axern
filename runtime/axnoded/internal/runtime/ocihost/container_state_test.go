package ocihost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePID(t *testing.T) {
	root := t.TempDir()
	common := &Common{containerRoot: root}
	containerID := "sandbox-1"
	if err := os.Mkdir(filepath.Join(root, containerID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(common.RuntimePIDFilePath(containerID), []byte("1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pid, err := common.RuntimePID(containerID)
	if err != nil {
		t.Fatalf("RuntimePID() error = %v", err)
	}
	if pid != 1234 {
		t.Fatalf("RuntimePID() = %d, want 1234", pid)
	}
}

func TestRuntimePIDRejectsInvalidValue(t *testing.T) {
	root := t.TempDir()
	common := &Common{containerRoot: root}
	containerID := "sandbox-1"
	if err := os.Mkdir(filepath.Join(root, containerID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(common.RuntimePIDFilePath(containerID), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := common.RuntimePID(containerID); err == nil {
		t.Fatal("RuntimePID() error = nil, want invalid pid rejection")
	}
}
