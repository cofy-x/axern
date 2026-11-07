//go:build linux

package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestNativeStateForgetRetiresOnlyExactInterruptedTemporaryState(t *testing.T) {
	f := newNativeForgetFixture(t)
	temporary := f.state + ".tmp"
	require.NoError(t, os.WriteFile(temporary, []byte(`{"status":"running",`), 0600))
	unrelated := filepath.Join(f.root, "other_sandbox:other.state.tmp")
	require.NoError(t, os.WriteFile(unrelated, []byte("unrelated"), 0600))
	require.NoError(t, f.forget(t.Context(), func(int) error { return nil }))
	require.NoFileExists(t, temporary)
	require.NoFileExists(t, f.state)
	require.NoFileExists(t, f.lock)
	require.FileExists(t, unrelated)
}

func TestNativeStateForgetRejectsUnsafeTemporaryState(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "writable", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeForgetFixture(t)
			temporary := f.state + ".tmp"
			switch kind {
			case "symlink":
				require.NoError(t, os.Symlink(filepath.Join(f.root, "target"), temporary))
			case "hardlink":
				require.NoError(t, os.Link(f.state, temporary))
			case "writable":
				require.NoError(t, os.WriteFile(temporary, nil, 0600))
				require.NoError(t, os.Chmod(temporary, 0666))
			case "fifo":
				require.NoError(t, unix.Mkfifo(temporary, 0600))
			case "directory":
				require.NoError(t, os.Mkdir(temporary, 0700))
			}
			require.Error(t, f.forget(t.Context(), func(int) error { t.Fatal("unsafe temporary state must not reach kernel proof"); return nil }))
			require.FileExists(t, f.state)
			require.FileExists(t, f.lock)
			_, err := os.Lstat(temporary)
			require.NoError(t, err)
		})
	}
}

func TestNativeStateForgetRetainsOwnershipWhenTemporaryStateChanges(t *testing.T) {
	for _, initiallyPresent := range []bool{false, true} {
		t.Run(map[bool]string{false: "appeared", true: "replaced"}[initiallyPresent], func(t *testing.T) {
			f := newNativeForgetFixture(t)
			temporary := f.state + ".tmp"
			if initiallyPresent {
				require.NoError(t, os.WriteFile(temporary, nil, 0600))
			}
			require.Error(t, f.forget(t.Context(), func(int) error {
				if initiallyPresent {
					require.NoError(t, os.Rename(temporary, temporary+".old"))
				}
				require.NoError(t, os.WriteFile(temporary, []byte("replacement"), 0600))
				return nil
			}))
			require.FileExists(t, temporary)
			require.FileExists(t, f.state)
			require.FileExists(t, f.lock)
		})
	}
}

func TestNativeTemporaryStateAloneNeverAuthorizesCleanup(t *testing.T) {
	f := newNativeForgetFixture(t)
	temporary := f.state + ".tmp"
	require.NoError(t, os.WriteFile(temporary, nil, 0600))
	require.NoError(t, os.Remove(f.state))
	require.NoError(t, f.forget(t.Context(), func(int) error { t.Fatal("missing primary state cannot grant cleanup authority"); return nil }))
	require.FileExists(t, temporary)
	require.FileExists(t, f.lock)
}
