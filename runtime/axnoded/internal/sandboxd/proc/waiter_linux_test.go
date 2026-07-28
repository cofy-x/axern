//go:build linux

package proc

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestProcessWaiterReplaysCachedExitStatus(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	cmd := exec.Command("/bin/sh", "-c", "exit 23")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	cached := false
	for time.Now().Before(deadline) {
		waiter.ReapAvailable()
		waiter.mu.Lock()
		_, ok := waiter.cache[cmd.Process.Pid]
		waiter.mu.Unlock()
		if ok {
			cached = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cached {
		t.Fatal("timed out waiting for cached exit status")
	}

	result := <-waiter.Watch(cmd)
	if result.ExitCode != 23 {
		t.Fatalf("exit code = %d, want 23", result.ExitCode)
	}
}

func TestProcessWaiterFallbackCachesExitStatus(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	cmd := exec.Command("/bin/sh", "-c", "exit 31")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		waiter.mu.Lock()
		_, ok := waiter.cache[cmd.Process.Pid]
		waiter.mu.Unlock()
		if ok {
			result := <-waiter.Watch(cmd)
			if result.ExitCode != 31 {
				t.Fatalf("exit code = %d, want 31", result.ExitCode)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for fallback reaper to cache exit status")
}

func TestProcessWaiterWatchReapsAlreadyExitedProcess(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	cmd := exec.Command("/bin/sh", "-c", "exit 29")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	select {
	case result := <-waiter.Watch(cmd):
		if result.ExitCode != 29 {
			t.Fatalf("exit code = %d, want 29", result.ExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for already-exited process")
	}
}
