//go:build linux

package rootfsview

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEROFSIsUsableAsImmutableProjectionLower(t *testing.T) {
	required := os.Getenv("AXERN_VERIFY_EROFS") == "1"
	if os.Geteuid() != 0 {
		if required {
			t.Fatal("AXERN_VERIFY_EROFS requires root")
		}
		t.Skip("EROFS projection integration requires root")
	}
	mkfs, err := exec.LookPath("mkfs.erofs")
	if err != nil {
		if required {
			t.Fatal("AXERN_VERIFY_EROFS requires mkfs.erofs")
		}
		t.Skip("mkfs.erofs is unavailable")
	}

	work := t.TempDir()
	source := filepath.Join(work, "source")
	lower := filepath.Join(work, "lower")
	filestore := filepath.Join(work, "filestore")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "bin"), 0755))
	require.NoError(t, os.MkdirAll(lower, 0755))
	require.NoError(t, os.MkdirAll(filestore, 0755))
	filestoreImage := filepath.Join(work, "filestore.ext4")
	require.NoError(t, os.WriteFile(filestoreImage, nil, 0600))
	require.NoError(t, os.Truncate(filestoreImage, 128<<20))
	output, err := exec.Command("mkfs.ext4", "-F", filestoreImage).CombinedOutput()
	require.NoErrorf(t, err, "mkfs.ext4: %s", output)
	output, err = exec.Command("mount", "-t", "ext4", "-o", "loop", filestoreImage, filestore).CombinedOutput()
	require.NoErrorf(t, err, "mount ext4 filestore: %s", output)
	t.Cleanup(func() { _ = exec.Command("umount", filestore).Run() })
	require.NoError(t, os.WriteFile(filepath.Join(source, "bin", "fixture"), []byte("immutable\n"), 0555))
	require.NoError(t, os.WriteFile(filepath.Join(source, "bin", "delete-me"), []byte("lower-only\n"), 0444))
	image := filepath.Join(work, "rootfs.erofs")
	output, err = exec.Command(mkfs, image, source).CombinedOutput()
	require.NoErrorf(t, err, "mkfs.erofs: %s", output)
	imageBefore, err := os.ReadFile(image)
	require.NoError(t, err)
	hashBefore := sha256.Sum256(imageBefore)
	output, err = exec.Command("mount", "-t", "erofs", "-o", "loop,ro", image, lower).CombinedOutput()
	require.NoErrorf(t, err, "mount erofs: %s", output)
	t.Cleanup(func() { _ = exec.Command("umount", lower).Run() })

	backing, err := InspectBacking(lower)
	require.NoError(t, err)
	require.Equal(t, "erofs", backing.FSType)
	provider := NewOverlayProvider(filestore)
	view, err := provider.Prepare(context.Background(), "erofs-fixture", Request{
		RootDir: lower, RuntimeName: "runsc", Backing: backing,
		Targets: []MountTarget{{Destination: "/etc/hosts", Kind: TargetRegularFile}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Remove(context.Background(), "erofs-fixture") })
	require.True(t, view.Prepared)
	content, err := os.ReadFile(filepath.Join(view.RootDir, "bin", "fixture"))
	require.NoError(t, err)
	require.Equal(t, "immutable\n", string(content))
	require.NoError(t, os.WriteFile(filepath.Join(view.RootDir, "bin", "fixture"), []byte("copy-up\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(view.RootDir, "created"), []byte("upper-only\n"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(view.RootDir, "created-dir"), 0755))
	require.NoError(t, os.Remove(filepath.Join(view.RootDir, "bin", "delete-me")))
	content, err = os.ReadFile(filepath.Join(lower, "bin", "fixture"))
	require.NoError(t, err)
	require.Equal(t, "immutable\n", string(content))
	content, err = os.ReadFile(filepath.Join(lower, "bin", "delete-me"))
	require.NoError(t, err)
	require.Equal(t, "lower-only\n", string(content))
	info, err := os.Stat(filepath.Join(view.RootDir, "etc", "hosts"))
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	_, err = os.Stat(filepath.Join(lower, "etc"))
	require.True(t, os.IsNotExist(err), "EROFS lower was unexpectedly modified")
	imageAfter, err := os.ReadFile(image)
	require.NoError(t, err)
	require.Equal(t, hashBefore, sha256.Sum256(imageAfter), "EROFS fixture image changed during projection")
}
