//go:build linux

package runtime

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func newNativeSocketFixture(t *testing.T) nativeForgetFixture {
	t.Helper()
	f := newNativeForgetFixture(t)
	oldState, oldLock := f.state, f.lock
	f.id = fmt.Sprintf("axn-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	f.state = filepath.Join(f.root, f.id+"_sandbox:"+f.id+".state")
	f.lock = filepath.Join(f.root, f.id+"_sandbox:"+f.id+".lock")
	f.value["id"] = f.id
	f.value["sandbox"].(map[string]any)["id"] = f.id
	f.value["sandbox"].(map[string]any)["controlSocketPath"] = filepath.Join(f.root, "runsc-"+f.id+".sock")
	f.value["saver"].(map[string]any)["id"] = map[string]any{"sandboxId": f.id, "containerId": f.id}
	f.write(t)
	require.NoError(t, os.Remove(oldState))
	require.NoError(t, os.Rename(oldLock, f.lock))
	return f
}

// Bind through a parent FD, as the pinned runtime does, so this fixture also
// exercises control socket paths longer than sockaddr_un's pathname limit.
func createNativeTestSocket(t *testing.T, path string, live bool) *net.UnixListener {
	t.Helper()
	parent, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	defer unix.Close(parent)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: fmt.Sprintf("/proc/self/fd/%d/%s", parent, filepath.Base(path)), Net: "unix"})
	require.NoError(t, err)
	listener.SetUnlinkOnClose(false)
	require.NoError(t, os.Chmod(path, 0600))
	t.Cleanup(func() { _ = listener.Close(); _ = os.Remove(path) })
	if !live {
		require.NoError(t, listener.Close())
	}
	return listener
}

func TestNativeStateForgetRetiresOnlySavedDeadControlSocket(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			f := newNativeSocketFixture(t)
			name := "runsc-" + f.id + ".sock"
			rootPath, fallbackPath := filepath.Join(f.root, name), filepath.Join("/tmp", name)
			createNativeTestSocket(t, rootPath, false)
			createNativeTestSocket(t, fallbackPath, false)
			saved, untouched := rootPath, fallbackPath
			if fallback {
				saved, untouched = fallbackPath, rootPath
			}
			f.value["sandbox"].(map[string]any)["controlSocketPath"] = saved
			f.write(t)
			require.NoError(t, f.forget(t.Context(), func(int) error { return nil }))
			require.NoFileExists(t, saved)
			require.FileExists(t, untouched)
			require.NoFileExists(t, f.state)
			require.NoFileExists(t, f.lock)
			if !fallback {
				require.Greater(t, len(rootPath), 108, "fixture must exercise the pinned FD long-path lookup")
			}
		})
	}
}

func TestNativeStateForgetRetainsLiveControlSocketAndOwnership(t *testing.T) {
	f := newNativeSocketFixture(t)
	path := f.value["sandbox"].(map[string]any)["controlSocketPath"].(string)
	createNativeTestSocket(t, path, true)
	require.ErrorContains(t, f.forget(t.Context(), func(int) error { return nil }), "live or unverifiable endpoint")
	require.FileExists(t, path)
	require.FileExists(t, f.state)
	require.FileExists(t, f.lock)
}

func TestNativeStateForgetRejectsChangedSocketDuringKernelProof(t *testing.T) {
	for _, initiallyMissing := range []bool{false, true} {
		t.Run(fmt.Sprintf("initiallyMissing=%t", initiallyMissing), func(t *testing.T) {
			f := newNativeSocketFixture(t)
			path := f.value["sandbox"].(map[string]any)["controlSocketPath"].(string)
			if !initiallyMissing {
				createNativeTestSocket(t, path, false)
			}
			err := f.forget(t.Context(), func(int) error {
				if !initiallyMissing {
					require.NoError(t, os.Rename(path, path+".old"))
				}
				createNativeTestSocket(t, path, false)
				return nil
			})
			require.Error(t, err)
			require.FileExists(t, path)
			require.FileExists(t, f.state)
			require.FileExists(t, f.lock)
		})
	}
}

func TestNativeStateForgetRejectsUnownedControlSocket(t *testing.T) {
	for _, kind := range []string{"missing saved path", "other allocation", "outside candidate", "regular file", "symlink", "hardlink", "writable socket"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeSocketFixture(t)
			path := f.value["sandbox"].(map[string]any)["controlSocketPath"].(string)
			switch kind {
			case "missing saved path":
				delete(f.value["sandbox"].(map[string]any), "controlSocketPath")
			case "other allocation":
				f.value["sandbox"].(map[string]any)["controlSocketPath"] = filepath.Join(f.root, "runsc-other.sock")
			case "outside candidate":
				f.value["sandbox"].(map[string]any)["controlSocketPath"] = filepath.Join(t.TempDir(), filepath.Base(path))
			case "regular file":
				require.NoError(t, os.WriteFile(path, []byte("unrelated"), 0600))
			case "symlink":
				require.NoError(t, os.Symlink(filepath.Join(f.root, "target"), path))
			case "hardlink":
				createNativeTestSocket(t, path, false)
				require.NoError(t, os.Link(path, path+".alias"))
			case "writable socket":
				createNativeTestSocket(t, path, false)
				require.NoError(t, os.Chmod(path, 0666))
			}
			f.write(t)
			require.Error(t, f.forget(t.Context(), func(int) error {
				t.Fatal("untrusted socket must fail before kernel cleanup proof")
				return nil
			}))
			require.FileExists(t, f.state)
			require.FileExists(t, f.lock)
		})
	}
}
