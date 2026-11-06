package sandboxd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func guestTaskControlFixture() map[string]string {
	return map[string]string{
		"/proc/self/cgroup":                         "0::/\n",
		"/proc/self/mountinfo":                      "1 0 0:1 / / rw - overlay overlay rw\n2 1 0:2 /allocation /sys/fs/cgroup ro,nosuid,nodev,noexec - cgroup2 cgroup2 ro,nsdelegate\n",
		guestCgroupPath + "/pids.max":               "64\n",
		guestCgroupPath + "/pids.current":           "8\n",
		guestCgroupPath + "/cgroup.max.depth":       "0\n",
		guestCgroupPath + "/cgroup.max.descendants": "0\n",
		guestCgroupPath + "/cgroup.stat":            "nr_descendants 0\nnr_dying_descendants 0\n",
	}
}

func guestTaskFixtureReader(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		value, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(value), nil
	}
}

func TestGuestTaskGuardAcceptsConfiguredBoundary(t *testing.T) {
	if err := waitForGuestTaskControls(context.Background(), 64, guestTaskFixtureReader(guestTaskControlFixture())); err != nil {
		t.Fatal(err)
	}
}

func TestGuestTaskGuardRejectsInvalidBoundary(t *testing.T) {
	for _, test := range []struct {
		name, path, value string
	}{
		{"membership outside namespace root", "/proc/self/cgroup", "0::/allocation\n"},
		{"multiple memberships", "/proc/self/cgroup", "0::/\n1:pids:/\n"},
		{"writable mount", "/proc/self/mountinfo", "2 1 0:2 /allocation /sys/fs/cgroup rw,nosuid,nodev,noexec - cgroup2 cgroup2 rw,nsdelegate\n"},
		{"wrong filesystem", "/proc/self/mountinfo", "2 1 0:2 / /sys/fs/cgroup ro - tmpfs tmpfs ro\n"},
		{"missing namespace delegation", "/proc/self/mountinfo", "2 1 0:2 / /sys/fs/cgroup ro - cgroup2 none ro\n"},
		{"missing mount", "/proc/self/mountinfo", "1 0 0:1 / / rw - overlay overlay rw\n"},
		{"malformed mountinfo", "/proc/self/mountinfo", "broken\n"},
		{"nested mount", "/proc/self/mountinfo", "2 1 0:2 /allocation /sys/fs/cgroup ro - cgroup2 cgroup2 rw,nsdelegate\n3 2 0:3 / /sys/fs/cgroup/pids.max ro - tmpfs tmpfs ro\n"},
		{"duplicate mount", "/proc/self/mountinfo", "2 1 0:2 /allocation /sys/fs/cgroup ro - cgroup2 cgroup2 rw,nsdelegate\n3 1 0:3 /allocation /sys/fs/cgroup ro - cgroup2 cgroup2 rw,nsdelegate\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := guestTaskControlFixture()
			files[test.path] = test.value
			if err := waitForGuestTaskControls(context.Background(), 64, guestTaskFixtureReader(files)); err == nil {
				t.Fatal("guard accepted invalid boundary")
			}
		})
	}
}

func TestGuestTaskGuardRejectsInvalidControls(t *testing.T) {
	for _, test := range []struct {
		name, file, value string
	}{
		{"different finite limit", "pids.max", "128\n"},
		{"zero limit", "pids.max", "0\n"},
		{"malformed limit", "pids.max", "64 128\n"},
		{"signed limit", "pids.max", "+64\n"},
		{"overflow limit", "pids.max", "18446744073709551615\n"},
		{"child depth permitted", "cgroup.max.depth", "1\n"},
		{"descendants permitted", "cgroup.max.descendants", "1\n"},
		{"too many existing tasks", "pids.current", "65\n"},
		{"missing PID1 charge", "pids.current", "0\n"},
		{"malformed current", "pids.current", "max\n"},
		{"existing descendant", "cgroup.stat", "nr_descendants 1\nnr_dying_descendants 0\n"},
		{"missing descendant count", "cgroup.stat", "nr_dying_descendants 0\n"},
		{"duplicate descendant count", "cgroup.stat", "nr_descendants 0\nnr_descendants 0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := guestTaskControlFixture()
			files[guestCgroupPath+"/"+test.file] = test.value
			if err := waitForGuestTaskControls(context.Background(), 64, guestTaskFixtureReader(files)); err == nil {
				t.Fatal("guard accepted invalid controls")
			}
		})
	}
}

func TestGuestTaskGuardWaitsForNodeControls(t *testing.T) {
	files := guestTaskControlFixture()
	readFile := guestTaskFixtureReader(files)
	reads := 0
	reader := func(path string) ([]byte, error) {
		if path == guestCgroupPath+"/pids.max" {
			reads++
			if reads == 1 {
				return []byte("max\n"), nil
			}
		}
		return readFile(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForGuestTaskControls(ctx, 64, reader); err != nil {
		t.Fatal(err)
	}
	if reads < 2 {
		t.Fatal("guard released before configured controls were observed")
	}
}

func TestGuestTaskGuardUnconfiguredControlsAreBounded(t *testing.T) {
	for _, control := range []string{"pids.max", "cgroup.max.depth", "cgroup.max.descendants"} {
		for _, missing := range []bool{false, true} {
			name := control
			if missing {
				name += " missing"
			}
			t.Run(name, func(t *testing.T) {
				files := guestTaskControlFixture()
				files[guestCgroupPath+"/"+control] = "max\n"
				if missing {
					delete(files, guestCgroupPath+"/"+control)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()
				err := waitForGuestTaskControls(ctx, 64, guestTaskFixtureReader(files))
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("error = %v, want bounded startup timeout", err)
				}
			})
		}
	}
}

func TestGuestTaskGuardReadFailureIsNotReadiness(t *testing.T) {
	readFile := guestTaskFixtureReader(guestTaskControlFixture())
	for _, path := range []string{"/proc/self/cgroup", "/proc/self/mountinfo", guestCgroupPath + "/pids.max", guestCgroupPath + "/pids.current", guestCgroupPath + "/cgroup.stat"} {
		t.Run(path, func(t *testing.T) {
			reader := func(name string) ([]byte, error) {
				if name == path {
					return nil, os.ErrPermission
				}
				return readFile(name)
			}
			if err := waitForGuestTaskControls(context.Background(), 64, reader); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("error = %v, want permission failure", err)
			}
		})
	}
}

func TestRunnerTaskGuardPrecedesListenerAndEntrypoint(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("test requires a non-PID1 process")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "entrypoint-ran")
	socket := filepath.Join(dir, "control", "sandboxd.sock")
	runner := NewRunner(Config{
		SocketPath:     socket,
		GuestTaskLimit: 64,
		Entrypoint: workload.Entrypoint{
			Args: []string{"/bin/sh", "-c", "touch \"$1\"", "sh", marker},
		},
	}, io.Discard, io.Discard)
	code, err := runner.Run(context.Background())
	if code != 1 || err == nil || !strings.Contains(err.Error(), "PID 1") {
		t.Fatalf("Run() = (%d, %v), want PID1 guard failure", code, err)
	}
	if runner.waiter != nil {
		t.Fatal("reaper initialized before the guard")
	}
	for _, path := range []string{filepath.Dir(socket), marker} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("guard failure created %s: %v", path, err)
		}
	}
}

func TestGuestTaskGuardDisabledPreservesCompatibility(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForGuestTaskLimit(ctx, 0); err != nil {
		t.Fatalf("disabled guard inspected the process or controls: %v", err)
	}
}
