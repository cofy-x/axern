//go:build linux

package allocation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/stretchr/testify/require"
)

func TestSecretFilesRetireOnlyAfterRuntimeDeletion(t *testing.T) {
	for _, failedStart := range []bool{false, true} {
		name := "normal-delete"
		if failedStart {
			name = "failed-start"
		}
		t.Run(name, func(t *testing.T) {
			handler := &runtimeSpyHandler{name: "runsc", deleteErrors: []error{errors.New("runtime still owns mounts")}}
			fixture := newTestAllocationController(t, handler)
			id := "allocation-secret-retirement"
			require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(time.Minute), nil, nil, nil, nil, nil))
			storeTestContainer(t, fixture, id, "runsc")
			mounts, _, err := startplan.MaterializeResolvedSecretFiles(fixture.controller.config.RootDir, &runtime.StartRequest{AllocationID: id, SecretFiles: []*runtime.ResolvedSecretFile{{Path: "/run/secrets/fixture", Content: []byte("inert-owned-value"), Mode: 0444}}})
			require.NoError(t, err)
			cleanup := func() error {
				if failedStart {
					return fixture.controller.CleanupPersistedFailedStart(t.Context(), id)
				}
				_, err := fixture.controller.DeleteControlPlane(t.Context(), &runtime.DeleteRequest{ID: id}, "node-a")
				return err
			}
			require.Error(t, cleanup())
			data, err := os.ReadFile(mounts[0].GetSource())
			require.NoError(t, err)
			require.Equal(t, "inert-owned-value", string(data), "an ambiguous runtime retains mounted plaintext and its cleanup owner")
			require.True(t, fixture.controller.HasAdmittedAllocation(id))
			require.NoError(t, cleanup())
			_, err = os.Stat(mounts[0].GetSource())
			require.ErrorIs(t, err, os.ErrNotExist)
			require.False(t, fixture.controller.HasAdmittedAllocation(id))
		})
	}
}

func TestSecretCleanupFailureRetainsAdmissionForRetry(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	id := "allocation-secret-cleanup-debt"
	require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(time.Minute), nil, nil, nil, nil, nil))
	storeTestContainer(t, fixture, id, "runsc")
	mounts, _, err := startplan.MaterializeResolvedSecretFiles(fixture.controller.config.RootDir, &runtime.StartRequest{AllocationID: id, SecretFiles: []*runtime.ResolvedSecretFile{{Path: "/run/secrets/fixture", Content: []byte("inert-owned-value"), Mode: 0444}}})
	require.NoError(t, err)
	dir := filepath.Dir(mounts[0].GetSource())
	outside := t.TempDir()
	require.NoError(t, os.Rename(dir, dir+"-retained"))
	require.NoError(t, os.Symlink(outside, dir))
	_, err = fixture.controller.DeleteControlPlane(t.Context(), &runtime.DeleteRequest{ID: id}, "node-a")
	require.Error(t, err)
	require.True(t, fixture.controller.HasAdmittedAllocation(id), "runtime absence cannot discard unresolved plaintext cleanup")
	require.NoError(t, os.Remove(dir))
	require.NoError(t, os.Rename(dir+"-retained", dir))
	_, err = fixture.controller.DeleteControlPlane(t.Context(), &runtime.DeleteRequest{ID: id}, "node-a")
	require.NoError(t, err)
	require.False(t, fixture.controller.HasAdmittedAllocation(id))
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries)
}
