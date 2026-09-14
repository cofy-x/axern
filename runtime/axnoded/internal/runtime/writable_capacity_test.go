package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWritableCapacityManager(t *testing.T, systemReserve int64) *writableCapacityManager {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "allocation-charges")
	manager := &writableCapacityManager{
		dir:           dir,
		systemReserve: systemReserve,
		charges:       make(map[string]writableCharge),
	}
	require.NoError(t, manager.load())
	return manager
}

func TestWritableCapacityChargeIsDurableAndIdempotent(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)

	require.NoError(t, manager.Charge("sandbox-1", "runsc", 4096, 8192))
	require.NoError(t, manager.Charge("sandbox-1", "runsc", 4096, 8192))
	require.ErrorContains(t, manager.Charge("sandbox-1", "runsc", 4096, 16384), "different writable charge")

	reloaded := &writableCapacityManager{
		dir:     manager.dir,
		charges: make(map[string]writableCharge),
	}
	require.NoError(t, reloaded.load())
	assert.Equal(t, int64(4096), reloaded.charges["sandbox-1"].RequestBytes)

	require.NoError(t, reloaded.Release("sandbox-1"))
	_, err := os.Stat(filepath.Join(manager.dir, "sandbox-1.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, reloaded.Release("sandbox-1"))
}

func TestWritableCapacityChargeRejectsUnsafeContainerID(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)

	require.ErrorContains(t, manager.Charge("../escape", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Charge("", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Charge("escape;command", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Release("../escape"), "invalid container ID")
}

func TestWritableCapacityChargeRejectsFilenameMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "allocation-charges")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "safe.json"), []byte(`{
  "container_id": "../escape",
  "request_bytes": 1,
  "limit_bytes": 1
}`), 0600))

	manager := &writableCapacityManager{dir: dir, charges: make(map[string]writableCharge)}
	require.ErrorContains(t, manager.load(), "invalid writable charge")
}

func TestWritableCapacityChargeRejectsRemovedRuntimeIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "allocation-charges")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sandbox.json"), []byte(`{
  "container_id": "sandbox",
  "runtime_name": "runsc",
  "request_bytes": 1,
  "limit_bytes": 1
}`), 0600))

	manager := &writableCapacityManager{dir: dir, charges: make(map[string]writableCharge)}
	require.ErrorContains(t, manager.load(), `unknown field "runtime_name"`)
}

func TestWritableCapacityChargeEnforcesLiveAvailableFloor(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 1<<62)

	require.ErrorContains(t, manager.Charge("sandbox-1", "runsc", 1, 1), "insufficient ephemeral storage capacity")
}

func TestWritableCapacityReconcileCleansAllStaleCharges(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)
	require.NoError(t, manager.Charge("active-runsc", "runsc", 1, 1))
	require.NoError(t, manager.Charge("stale-runsc", "runsc", 1, 1))
	require.NoError(t, manager.Charge("stale-second", "runsc", 1, 1))
	cleaned := make([]string, 0)

	require.NoError(t, manager.Reconcile(map[string]struct{}{"active-runsc": {}}, func(id string) error {
		cleaned = append(cleaned, id)
		return nil
	}))
	assert.Equal(t, []string{"stale-runsc", "stale-second"}, cleaned)
	assert.Contains(t, manager.charges, "active-runsc")
	assert.NotContains(t, manager.charges, "stale-runsc")
	assert.NotContains(t, manager.charges, "stale-second")
}
