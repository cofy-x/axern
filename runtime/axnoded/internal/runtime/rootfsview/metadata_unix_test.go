//go:build unix

package rootfsview

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectionRootPreservesGuestMetadataUnderPrivateUmask(t *testing.T) {
	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)
	for _, mode := range []os.FileMode{0755, 0750 | os.ModeSticky} {
		t.Run(mode.String(), func(t *testing.T) {
			lower := t.TempDir()
			require.NoError(t, os.Chmod(lower, mode))
			// Existing image parents can be copied by the host test's user.
			// New guest directories intentionally require root:root ownership.
			require.NoError(t, os.Mkdir(filepath.Join(lower, "reference"), 0755))
			view := overlayViewForContainer("metadata", t.TempDir(), projectionViewDir, []string{lower})
			require.NoError(t, initializeOverlayView(view, lower))
			require.NoError(t, seedMountTargets(lower, view.UpperDir,
				[]MountTarget{{Destination: "/reference/input", Kind: TargetRegularFile}}))
			require.FileExists(t, filepath.Join(view.UpperDir, "reference/input"))
			require.NoFileExists(t, filepath.Join(lower, "reference/input"))
			upperInfo, err := os.Stat(view.UpperDir)
			require.NoError(t, err)
			lowerInfo, err := os.Stat(lower)
			require.NoError(t, err)
			require.Equal(t, lowerInfo.Mode(), upperInfo.Mode())
			want := lowerInfo.Sys().(*syscall.Stat_t)
			got := upperInfo.Sys().(*syscall.Stat_t)
			require.Equal(t, want.Uid, got.Uid)
			require.Equal(t, want.Gid, got.Gid)
			for _, dir := range []string{filepath.Dir(view.UpperDir), view.WorkDir, view.MergedDir} {
				info, err := os.Stat(dir)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0700), info.Mode().Perm())
			}
		})
	}
}

func TestProjectionRootCopiesNonRootOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("creating foreign-owned root metadata requires root")
	}
	lower := t.TempDir()
	require.NoError(t, os.Chmod(lower, 0755))
	require.NoError(t, os.Chown(lower, 12345, 12346))
	view := overlayViewForContainer("ownership", t.TempDir(), projectionViewDir, []string{lower})
	require.NoError(t, initializeOverlayView(view, lower))
	info, err := os.Stat(view.UpperDir)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	require.EqualValues(t, 12345, stat.Uid)
	require.EqualValues(t, 12346, stat.Gid)
}
