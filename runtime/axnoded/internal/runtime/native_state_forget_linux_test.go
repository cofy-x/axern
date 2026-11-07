//go:build linux

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type nativeForgetFixture struct {
	root, id, bundle, state, lock string
	value                         map[string]any
}

func newNativeForgetFixture(t *testing.T) nativeForgetFixture {
	t.Helper()
	root := t.TempDir()
	id := "allocation-1"
	f := nativeForgetFixture{root: root, id: id, bundle: filepath.Join(root, "bundle")}
	f.state = filepath.Join(root, id+"_sandbox:"+id+".state")
	f.lock = filepath.Join(root, id+"_sandbox:"+id+".lock")
	f.value = map[string]any{
		"id": id, "bundleDir": f.bundle, "status": "paused", "goferPid": os.Getpid(),
		"sandbox": map[string]any{"id": id, "pid": 4242, "controlSocketPath": filepath.Join(root, "runsc-"+id+".sock")},
		"saver":   map[string]any{"rootDir": root, "id": map[string]any{"sandboxId": id, "containerId": id}},
		"spec":    map[string]any{"annotations": map[string]string{"Key": "upper", "key": "lower"}},
	}
	f.write(t)
	require.NoError(t, os.WriteFile(f.lock, nil, 0600))
	return f
}

func (f nativeForgetFixture) write(t *testing.T) {
	t.Helper()
	payload, err := json.Marshal(f.value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.state, payload, 0600))
}

func (f nativeForgetFixture) forget(ctx context.Context, proof func(int) error) error {
	return forgetNativeRuntimeState(ctx, f.root, f.id, f.bundle, proof)
}

func TestNativeStateForgetUsesLockedKernelProofAndPreservesOtherFiles(t *testing.T) {
	for _, status := range []string{"created", "running", "paused", "stopped"} {
		t.Run(status, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			f.value["status"] = status
			f.write(t)
			other := filepath.Join(f.root, "another_sandbox:another.state")
			require.NoError(t, os.WriteFile(other, []byte("unrelated"), 0600))
			calls := 0
			require.NoError(t, f.forget(t.Context(), func(pid int) error {
				calls++
				require.Equal(t, 4242, pid)
				fd, err := unix.Open(f.lock, unix.O_RDWR, 0)
				require.NoError(t, err)
				defer unix.Close(fd)
				require.ErrorIs(t, unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK)
				return nil
			}))
			require.Equal(t, 1, calls)
			require.NoFileExists(t, f.state)
			require.NoFileExists(t, f.lock)
			payload, err := os.ReadFile(other)
			require.NoError(t, err)
			require.Equal(t, "unrelated", string(payload))
		})
	}
}

func TestNativeStateForgetNeverSignalsReusedCachedGoferPID(t *testing.T) {
	sentinel := exec.Command("sleep", "30")
	require.NoError(t, sentinel.Start())
	t.Cleanup(func() { _ = sentinel.Process.Kill(); _ = sentinel.Wait() })
	f := newNativeForgetFixture(t)
	f.value["goferPid"] = sentinel.Process.Pid
	f.write(t)
	require.NoError(t, f.forget(t.Context(), func(int) error { return nil }))
	require.NoError(t, unix.Kill(sentinel.Process.Pid, 0), "unrelated live cached PID must survive state retirement")
}

func TestNativeStateForgetRejectsConflictingIdentityWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*nativeForgetFixture)
	}{
		{"allocation", func(f *nativeForgetFixture) { f.value["id"] = "other" }},
		{"sandbox", func(f *nativeForgetFixture) { f.value["sandbox"].(map[string]any)["id"] = "other" }},
		{"pid", func(f *nativeForgetFixture) { f.value["sandbox"].(map[string]any)["pid"] = 0 }},
		{"bundle", func(f *nativeForgetFixture) { f.value["bundleDir"] = "/unrelated/bundle" }},
		{"saved root", func(f *nativeForgetFixture) { f.value["saver"].(map[string]any)["rootDir"] = "/unrelated/runtime" }},
		{"full sandbox id", func(f *nativeForgetFixture) {
			f.value["saver"].(map[string]any)["id"].(map[string]any)["sandboxId"] = "other"
		}},
		{"full container id", func(f *nativeForgetFixture) {
			f.value["saver"].(map[string]any)["id"].(map[string]any)["containerId"] = "other"
		}},
		{"unknown status", func(f *nativeForgetFixture) { f.value["status"] = "unknown" }},
		{"missing saver", func(f *nativeForgetFixture) { delete(f.value, "saver") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			test.change(&f)
			f.write(t)
			require.Error(t, f.forget(t.Context(), func(int) error { t.Fatal("invalid state must not reach kernel proof"); return nil }))
			require.FileExists(t, f.state)
			require.FileExists(t, f.lock)
		})
	}
}

func TestNativeStateForgetRejectsSymlinksAndWritableOrLinkedFiles(t *testing.T) {
	for _, kind := range []string{"root symlink", "state symlink", "lock symlink", "state hardlink", "lock hardlink", "state fifo", "lock fifo", "state directory", "lock directory", "writable state", "writable lock", "writable root"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			switch kind {
			case "root symlink":
				link := filepath.Join(t.TempDir(), "runtime")
				require.NoError(t, os.Symlink(f.root, link))
				f.root = link
			case "state symlink", "lock symlink":
				path := f.state
				if kind == "lock symlink" {
					path = f.lock
				}
				require.NoError(t, os.Rename(path, path+".target"))
				require.NoError(t, os.Symlink(path+".target", path))
			case "state hardlink", "lock hardlink":
				path := f.state
				if kind == "lock hardlink" {
					path = f.lock
				}
				require.NoError(t, os.Link(path, path+".alias"))
			case "state fifo", "lock fifo", "state directory", "lock directory":
				path := f.state
				if strings.HasPrefix(kind, "lock") {
					path = f.lock
				}
				require.NoError(t, os.Remove(path))
				if strings.HasSuffix(kind, "fifo") {
					require.NoError(t, unix.Mkfifo(path, 0600))
				} else {
					require.NoError(t, os.Mkdir(path, 0700))
				}
			case "writable state":
				require.NoError(t, os.Chmod(f.state, 0660))
			case "writable lock":
				require.NoError(t, os.Chmod(f.lock, 0660))
			case "writable root":
				require.NoError(t, os.Chmod(f.root, 0770))
			}
			require.Error(t, f.forget(t.Context(), func(int) error { t.Fatal("untrusted files must not reach proof"); return nil }))
			_, err := os.Lstat(f.state)
			require.NoError(t, err)
			_, err = os.Lstat(f.lock)
			require.NoError(t, err)
		})
	}
}

func TestNativeStateForgetRechecksFilesAfterKernelProof(t *testing.T) {
	for _, name := range []string{"state", "lock", "root"} {
		t.Run(name, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			err := f.forget(t.Context(), func(int) error {
				if name == "root" {
					require.NoError(t, os.Rename(f.root, f.root+".original"))
					t.Cleanup(func() { _ = os.RemoveAll(f.root + ".original") })
					require.NoError(t, os.Mkdir(f.root, 0700))
					return nil
				}
				path := f.state
				if name == "lock" {
					path = f.lock
				}
				require.NoError(t, os.Rename(path, path+".original"))
				require.NoError(t, os.WriteFile(path, []byte("replacement"), 0600))
				return nil
			})
			require.ErrorContains(t, err, "replaced")
			if name != "root" {
				require.FileExists(t, f.state)
				require.FileExists(t, f.lock)
			}
		})
	}
}

func TestNativeStateForgetRequiresKernelProofAndBoundedLock(t *testing.T) {
	f := newNativeForgetFixture(t)
	proofErr := errors.New("original cgroup is not empty")
	require.ErrorIs(t, f.forget(t.Context(), func(int) error { return proofErr }), proofErr)
	require.FileExists(t, f.state)
	require.FileExists(t, f.lock)
	fd, err := unix.Open(f.lock, unix.O_RDWR, 0)
	require.NoError(t, err)
	defer unix.Close(fd)
	require.NoError(t, unix.Flock(fd, unix.LOCK_EX))
	defer unix.Flock(fd, unix.LOCK_UN)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, f.forget(ctx, func(int) error { t.Fatal("proof must hold the lock"); return nil }), context.DeadlineExceeded)
	require.FileExists(t, f.state)
}

func TestNativeStateForgetHandlesAbsenceWithoutInventingIdentity(t *testing.T) {
	f := newNativeForgetFixture(t)
	require.NoError(t, os.Remove(f.state))
	proof := func(int) error { t.Fatal("absent state has no cached process identity"); return nil }
	require.NoError(t, f.forget(t.Context(), proof))
	require.FileExists(t, f.lock, "a lock left by partial retirement is harmless and remains untouched")
	require.NoError(t, os.Remove(f.lock))
	require.NoError(t, f.forget(t.Context(), proof))
	f.write(t)
	require.ErrorContains(t, f.forget(t.Context(), proof), "existing native runtime state lock")
	require.FileExists(t, f.state)
}

func TestNativeStateForgetRejectsAmbiguousJSONAndUnsafeArguments(t *testing.T) {
	f := newNativeForgetFixture(t)
	payload, err := os.ReadFile(f.state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.state, []byte(strings.Replace(string(payload), `"id":"allocation-1"`, `"id":"allocation-1","ID":"other"`, 1)), 0600))
	require.ErrorContains(t, f.forget(t.Context(), func(int) error { return nil }), "duplicate")
	require.Error(t, forgetNativeRuntimeState(t.Context(), f.root, "../allocation", f.bundle, func(int) error { return nil }))
	require.Error(t, forgetNativeRuntimeState(t.Context(), f.root, f.id, "../bundle", func(int) error { return nil }))
	require.Error(t, forgetNativeRuntimeState(t.Context(), "relative", f.id, f.bundle, func(int) error { return nil }))
	require.Error(t, forgetNativeRuntimeState(t.Context(), f.root, f.id, f.bundle, nil))
	require.FileExists(t, f.state)
	require.FileExists(t, f.lock)
}

func TestNativeRuntimeStateAbsenceOnlyReadsTheExactTrustedRoot(t *testing.T) {
	f := newNativeForgetFixture(t)
	absent, err := nativeRuntimeStateAbsent(t.Context(), f.root, f.id)
	require.NoError(t, err)
	require.False(t, absent)
	require.NoError(t, os.Remove(f.state))
	other := filepath.Join(f.root, "unrelated.state")
	require.NoError(t, os.WriteFile(other, []byte("keep"), 0600))
	before, err := os.ReadDir(f.root)
	require.NoError(t, err)
	absent, err = nativeRuntimeStateAbsent(t.Context(), f.root, f.id)
	require.NoError(t, err)
	require.True(t, absent)
	after, err := os.ReadDir(f.root)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.FileExists(t, f.lock)
	payload, err := os.ReadFile(other)
	require.NoError(t, err)
	require.Equal(t, "keep", string(payload))
}

func TestNativeRuntimeStateAbsenceFailsClosedForUntrustedPaths(t *testing.T) {
	for _, kind := range []string{"missing root", "symlink root", "writable root", "symlink state", "directory state", "hardlinked state", "writable state", "invalid id", "relative root"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			switch kind {
			case "missing root":
				f.root = filepath.Join(t.TempDir(), "missing")
			case "symlink root":
				link := filepath.Join(t.TempDir(), "runtime")
				require.NoError(t, os.Symlink(f.root, link))
				f.root = link
			case "writable root":
				require.NoError(t, os.Chmod(f.root, 0770))
			case "symlink state":
				require.NoError(t, os.Remove(f.state))
				require.NoError(t, os.Symlink(filepath.Join(f.root, "absent-target"), f.state))
			case "directory state":
				require.NoError(t, os.Remove(f.state))
				require.NoError(t, os.Mkdir(f.state, 0700))
			case "hardlinked state":
				require.NoError(t, os.Link(f.state, f.state+".alias"))
			case "writable state":
				require.NoError(t, os.Chmod(f.state, 0660))
			case "invalid id":
				f.id = "../another"
			case "relative root":
				f.root = "relative"
			}
			absent, err := nativeRuntimeStateAbsent(t.Context(), f.root, f.id)
			require.Error(t, err)
			require.False(t, absent)
			require.FileExists(t, f.lock)
		})
	}
}

func TestNativeRuntimeStateAbsenceHonorsCanceledOrMissingContext(t *testing.T) {
	f := newNativeForgetFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	absent, err := nativeRuntimeStateAbsent(ctx, f.root, f.id)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, absent)
	absent, err = nativeRuntimeStateAbsent(nil, f.root, f.id)
	require.Error(t, err)
	require.False(t, absent)
	require.FileExists(t, f.state)
	require.FileExists(t, f.lock)
}
