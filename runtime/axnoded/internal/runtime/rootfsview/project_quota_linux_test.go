//go:build linux

package rootfsview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXFSProjectQuotaCoversOverlayCopyUpWorkdir(t *testing.T) {
	required := os.Getenv("AXERN_VERIFY_XFS_PROJECT_QUOTA") == "1"
	if os.Geteuid() != 0 {
		if required {
			t.Fatal("AXERN_VERIFY_XFS_PROJECT_QUOTA requires root")
		}
		t.Skip("XFS project quota integration requires root")
	}
	for _, command := range []string{"mkfs.xfs", "mount", "umount", "xfs_quota"} {
		if _, err := exec.LookPath(command); err != nil {
			if required {
				t.Fatalf("AXERN_VERIFY_XFS_PROJECT_QUOTA requires %s", command)
			}
			t.Skipf("%s is unavailable", command)
		}
	}

	work := t.TempDir()
	lower := filepath.Join(work, "lower")
	filestore := filepath.Join(work, "filestore")
	require.NoError(t, os.MkdirAll(lower, 0755))
	require.NoError(t, os.MkdirAll(filestore, 0755))

	output, err := exec.Command("mount", "-t", "tmpfs", "-o", "size=16m", "tmpfs", lower).CombinedOutput()
	require.NoErrorf(t, err, "mount tmpfs lower: %s", output)
	t.Cleanup(func() {
		if output, err := exec.Command("umount", lower).CombinedOutput(); err != nil {
			t.Errorf("unmount tmpfs lower: %s: %v", output, err)
		}
	})
	payloadPath := filepath.Join(lower, "copy-up-payload.bin")
	require.NoError(t, os.WriteFile(payloadPath, make([]byte, 4096), 0644))
	output, err = exec.Command("mount", "-o", "remount,ro", lower).CombinedOutput()
	require.NoErrorf(t, err, "remount tmpfs lower readonly: %s", output)

	filestoreImage := filepath.Join(work, "filestore.xfs")
	require.NoError(t, os.WriteFile(filestoreImage, nil, 0600))
	require.NoError(t, os.Truncate(filestoreImage, 512<<20))
	output, err = exec.Command("mkfs.xfs", "-f", filestoreImage).CombinedOutput()
	require.NoErrorf(t, err, "mkfs.xfs: %s", output)
	output, err = exec.Command("mount", "-t", "xfs", "-o", "loop,prjquota", filestoreImage, filestore).CombinedOutput()
	require.NoErrorf(t, err, "mount XFS filestore: %s", output)
	t.Cleanup(func() {
		if output, err := exec.Command("umount", filestore).CombinedOutput(); err != nil {
			t.Errorf("unmount XFS filestore: %s: %v", output, err)
		}
	})

	backing, err := InspectBacking(lower)
	require.NoError(t, err)
	provider := NewOverlayProvider(filestore)
	const (
		containerID = "xfs-copy-up"
		projectID   = uint32(300001)
		limitBytes  = int64(64 << 20)
	)
	view, err := provider.Prepare(context.Background(), containerID, Request{
		RootDir: lower, RuntimeName: "runc", NeedsHostWritableRootfs: true,
		ImmutableMount: backing.ImmutableMountDescriptor(""),
		ProjectID:      projectID, EphemeralStorageLimitBytes: limitBytes,
	})
	require.NoError(t, err)
	removed := false
	t.Cleanup(func() {
		if !removed {
			if err := provider.Remove(context.Background(), containerID); err != nil {
				t.Errorf("remove rootfs view: %v", err)
			}
		}
	})

	projectRoot := filepath.Dir(view.RootDir)
	require.NoError(t, VerifyProjectQuota(filestore, projectRoot, projectID, limitBytes))
	file, err := os.OpenFile(filepath.Join(view.RootDir, "copy-up-payload.bin"), os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	_, err = file.WriteAt([]byte("copy-up"), 0)
	require.NoError(t, err)
	require.NoError(t, file.Sync())
	require.NoError(t, file.Close())
	lowerContent, err := os.ReadFile(payloadPath)
	require.NoError(t, err)
	require.Equal(t, make([]byte, 4096), lowerContent)

	require.NoError(t, provider.Remove(context.Background(), containerID))
	removed = true
}
