package localstore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAcquireRunLockRejectsConcurrentOwner(t *testing.T) {
	runDir := t.TempDir()
	first, err := AcquireRunLock(runDir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	if _, err := AcquireRunLock(runDir); !errors.Is(err, ErrRunLocked) {
		t.Fatalf("second AcquireRunLock() error = %v, want ErrRunLocked", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireRunLock(runDir)
	if err != nil {
		t.Fatalf("AcquireRunLock() after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRunLockRejectsSymlink(t *testing.T) {
	runDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(runDir, ".lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireRunLock(runDir); err == nil {
		t.Fatal("AcquireRunLock() accepted symlink")
	}
}

func TestAcquireRunLockAcrossProcesses(t *testing.T) {
	if runDir := os.Getenv("AXRUN_LOCK_HELPER_DIR"); runDir != "" {
		lock, err := AcquireRunLock(runDir)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
		fmt.Println("locked")
		select {}
	}

	runDir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestAcquireRunLockAcrossProcesses$")
	command.Env = append(os.Environ(), "AXRUN_LOCK_HELPER_DIR="+runDir)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		t.Fatalf("helper did not acquire lock: line=%q error=%v", scanner.Text(), scanner.Err())
	}
	if _, err := AcquireRunLock(runDir); !errors.Is(err, ErrRunLocked) {
		t.Fatalf("AcquireRunLock() while child owns lock = %v, want ErrRunLocked", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	lock, err := AcquireRunLock(runDir)
	if err != nil {
		t.Fatalf("AcquireRunLock() after child exit: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}
