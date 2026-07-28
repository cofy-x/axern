package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestUploadDirMergesIntoExistingDirectory(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "existing.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	err := instance{root: root}.UploadDir(context.Background(), src, "/workspace", sandbox.UploadDirOptions{})
	if err != nil {
		t.Fatalf("UploadDir returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "workspace", "existing.txt")); err != nil {
		t.Fatalf("existing file was not preserved: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "workspace", "new.txt"))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("uploaded file = %q", data)
	}
}

func TestUploadDirNoOverwriteRejectsExistingDirectory(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "existing.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	err := instance{root: root}.UploadDir(context.Background(), src, "/workspace", sandbox.UploadDirOptions{NoOverwrite: true})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("UploadDir error = %v", err)
	}
}

func TestUploadDirWritableMakesUploadedTreeMutable(t *testing.T) {
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "bin"), 0o700); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "data.txt"), []byte("data"), 0o400); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "run.sh"), []byte("#!/bin/sh\n"), 0o500); err != nil {
		t.Fatalf("write executable file: %v", err)
	}
	root := t.TempDir()

	err := instance{root: root}.UploadDir(context.Background(), src, "/workspace", sandbox.UploadDirOptions{Writable: true})
	if err != nil {
		t.Fatalf("UploadDir returned error: %v", err)
	}
	for path, want := range map[string]os.FileMode{
		filepath.Join(root, "workspace"):                  0o777,
		filepath.Join(root, "workspace", "bin"):           0o777,
		filepath.Join(root, "workspace", "data.txt"):      0o666,
		filepath.Join(root, "workspace", "bin", "run.sh"): 0o777,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("mode %s = %o, want %o", path, got, want)
		}
	}
}

func TestDownloadPathCopiesFile(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "workspace", "answer.txt")
	if err := os.MkdirAll(filepath.Dir(remote), 0o755); err != nil {
		t.Fatalf("mkdir remote parent: %v", err)
	}
	if err := os.WriteFile(remote, []byte("answer\n"), 0o640); err != nil {
		t.Fatalf("write remote file: %v", err)
	}
	local := filepath.Join(t.TempDir(), "downloads", "answer.txt")

	if err := (instance{root: root}).DownloadPath(context.Background(), "/workspace/answer.txt", local, sandbox.DownloadPathOptions{}); err != nil {
		t.Fatalf("DownloadPath returned error: %v", err)
	}
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "answer\n" {
		t.Fatalf("downloaded data = %q", data)
	}
}

func TestDownloadPathCopiesDirectoryContents(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "workspace", "results")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatalf("mkdir remote directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(remote, "answer.txt"), []byte("answer\n"), 0o644); err != nil {
		t.Fatalf("write remote file: %v", err)
	}
	local := filepath.Join(t.TempDir(), "downloads", "results")

	if err := (instance{root: root}).DownloadPath(context.Background(), "/workspace/results", local, sandbox.DownloadPathOptions{}); err != nil {
		t.Fatalf("DownloadPath returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(local, "answer.txt"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "answer\n" {
		t.Fatalf("downloaded data = %q", data)
	}
}

func TestExecSupportsArgvCommandAndEnv(t *testing.T) {
	root := t.TempDir()
	result, err := instance{root: root}.Exec(
		context.Background(),
		sandbox.ArgvCommand([]string{"sh", "-c", "printf %s \"$AXRUN_TEST_VALUE\""}),
		sandbox.ExecOptions{
			Env: map[string]string{"AXRUN_TEST_VALUE": "ok"},
		},
	)
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout != "ok" || result.Stderr != "" {
		t.Fatalf("result = %#v", result)
	}
}
