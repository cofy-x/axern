//go:build linux

package startplan

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/require"
)

func secretFixtureRequest(id string) *runtime.StartRequest {
	return &runtime.StartRequest{AllocationID: id, SecretFiles: []*runtime.ResolvedSecretFile{{Path: "/run/secrets/fixture", Content: []byte("inert-test-value"), Mode: 0444}}}
}

func TestSecretMaterializationRejectsUntrustedParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, secretStorageName)))
	_, _, err := MaterializeResolvedSecretFiles(root, secretFixtureRequest("allocation-parent-substitution"))
	require.Error(t, err, "untrusted symlink parent must never authorize a host secret write")
	require.Error(t, CleanupResolvedSecretFiles(root, "allocation-parent-substitution"))
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries, "outside fixture must remain unchanged")
}

func TestSecretStorageIgnoresGlobalTemporaryDirectory(t *testing.T) {
	temporary, outside, root := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", temporary)
	require.NoError(t, os.Symlink(outside, filepath.Join(temporary, "axnoded-secrets")))
	request := secretFixtureRequest("allocation-private-node-root")
	mounts, _, err := MaterializeResolvedSecretFiles(root, request)
	require.NoError(t, err)
	require.Len(t, mounts, 1)
	require.Equal(t, filepath.Join(root, secretStorageName), filepath.Dir(filepath.Dir(mounts[0].GetSource())))
	require.NoError(t, CleanupResolvedSecretFiles(root, request.GetAllocationID()))
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSecretStorageRejectsUnsafeParentsWithoutAdoption(t *testing.T) {
	t.Run("writable vault", func(t *testing.T) {
		root := t.TempDir()
		vault := filepath.Join(root, secretStorageName)
		require.NoError(t, os.Mkdir(vault, 0700))
		require.NoError(t, os.Chmod(vault, 0777))
		_, _, err := MaterializeResolvedSecretFiles(root, secretFixtureRequest("allocation-unsafe-mode"))
		require.Error(t, err)
		info, err := os.Stat(vault)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0777), info.Mode().Perm(), "an unsafe existing parent is never chmod-adopted")
	})
	t.Run("configured root symlink", func(t *testing.T) {
		parent, outside := t.TempDir(), t.TempDir()
		root := filepath.Join(parent, "root")
		require.NoError(t, os.Symlink(outside, root))
		_, _, err := MaterializeResolvedSecretFiles(root, secretFixtureRequest("allocation-root-symlink"))
		require.Error(t, err)
		require.Error(t, CleanupResolvedSecretFiles(root, "allocation-root-symlink"))
		entries, err := os.ReadDir(outside)
		require.NoError(t, err)
		require.Empty(t, entries)
	})
	t.Run("wrong owner", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing fixture ownership requires root")
		}
		root := t.TempDir()
		vault := filepath.Join(root, secretStorageName)
		require.NoError(t, os.Mkdir(vault, 0700))
		require.NoError(t, os.Chown(vault, 65534, 65534))
		_, _, err := MaterializeResolvedSecretFiles(root, secretFixtureRequest("allocation-unsafe-owner"))
		require.Error(t, err)
	})
	t.Run("missing node root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "absent")
		require.Error(t, CleanupResolvedSecretFiles(root, "allocation-missing-root"), "missing data root is not cleanup proof")
	})
}

func TestSecretStorageRejectsPinnedDirectorySubstitution(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	marker := filepath.Join(outside, "marker")
	require.NoError(t, os.WriteFile(marker, []byte("unchanged"), 0600))
	request := secretFixtureRequest("allocation-directory-race")
	mounts, _, err := MaterializeResolvedSecretFiles(root, request)
	require.NoError(t, err)
	s, err := openSecretStorage(root, request.GetAllocationID(), false)
	require.NoError(t, err)
	defer s.close()
	original := filepath.Dir(mounts[0].GetSource())
	require.NoError(t, os.Rename(original, original+"-retained"))
	require.NoError(t, os.Symlink(outside, original))
	require.Error(t, s.write(secretKey("/new-target"), []byte("inert-replacement"), 0400))
	require.Error(t, s.remove())
	require.Error(t, CleanupResolvedSecretFiles(root, request.GetAllocationID()))
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(data))
	data, err = os.ReadFile(filepath.Join(original+"-retained", filepath.Base(mounts[0].GetSource())))
	require.NoError(t, err)
	require.Equal(t, "inert-test-value", string(data), "ambiguous path preserves the original owned plaintext for retry")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestSecretStoragePreservesGuestReadModeUnderPrivateServiceUmask(t *testing.T) {
	previous := syscall.Umask(0077)
	defer syscall.Umask(previous)
	root := t.TempDir()
	request := secretFixtureRequest("allocation-ordinary-user")
	mounts, _, err := MaterializeResolvedSecretFiles(root, request)
	require.NoError(t, err)
	info, err := os.Stat(mounts[0].GetSource())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0444), info.Mode().Perm(), "ordinary guest UIDs require the exact explicitly granted mode")
	for _, dir := range []string{filepath.Dir(mounts[0].GetSource()), filepath.Join(root, secretStorageName)} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0700), info.Mode().Perm(), "host users cannot traverse the plaintext parent")
	}
	require.Equal(t, []string{"ro"}, mounts[0].GetOptions())
	require.NoError(t, CleanupResolvedSecretFiles(root, request.GetAllocationID()))
}

func TestSecretStorageConcurrentAllocationsAndFreshAdmissionFence(t *testing.T) {
	root := t.TempDir()
	const workers = 8
	results := make(chan error, workers)
	var group sync.WaitGroup
	for i := range workers {
		group.Go(func() {
			request := secretFixtureRequest(fmt.Sprintf("allocation-concurrent-%d", i))
			mounts, _, err := MaterializeResolvedSecretFiles(root, request)
			if err == nil {
				_, err = os.ReadFile(mounts[0].GetSource())
			}
			if err == nil {
				err = CleanupResolvedSecretFiles(root, request.GetAllocationID())
			}
			results <- err
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	request := secretFixtureRequest("allocation-repeat")
	mounts, _, err := MaterializeResolvedSecretFiles(root, request)
	require.NoError(t, err)
	request.SecretFiles[0].Content = []byte("cannot-clobber")
	_, _, err = MaterializeResolvedSecretFiles(root, request)
	require.Error(t, err, "existing plaintext may still be mounted and is never replaced by another Admit")
	data, err := os.ReadFile(mounts[0].GetSource())
	require.NoError(t, err)
	require.Equal(t, "inert-test-value", string(data))
	require.NoError(t, CleanupResolvedSecretFiles(root, request.GetAllocationID()))
	require.NoError(t, CleanupResolvedSecretFiles(root, request.GetAllocationID()), "restart cleanup is idempotent")
}

func TestMaterializationCleanupOwnsOnlyItsCreatedDirectory(t *testing.T) {
	root := t.TempDir()
	request := secretFixtureRequest("allocation-cleanup-identity")
	mounts, cleanup, err := MaterializeResolvedSecretFiles(root, request)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	dir := filepath.Dir(mounts[0].GetSource())
	require.NoError(t, os.Rename(dir, dir+"-retained"))
	require.NoError(t, os.Mkdir(dir, 0700))
	marker := filepath.Join(dir, "replacement-marker")
	require.NoError(t, os.WriteFile(marker, []byte("unrelated-owned-data"), 0400))
	require.Error(t, cleanup(), "an attempt's cleanup cannot adopt a different directory identity")
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "unrelated-owned-data", string(data))
	_, err = os.Stat(filepath.Join(dir+"-retained", filepath.Base(mounts[0].GetSource())))
	require.NoError(t, err)
}
