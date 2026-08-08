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
	dir := filepath.Join(t.TempDir(), "reservations")
	manager := &writableCapacityManager{
		dir:           dir,
		systemReserve: systemReserve,
		reservations:  make(map[string]writableReservation),
	}
	require.NoError(t, manager.load())
	return manager
}

func TestWritableCapacityReservationIsDurableAndIdempotent(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)

	require.NoError(t, manager.Reserve("sandbox-1", "runc", 4096, 8192))
	projectID := manager.ProjectID("sandbox-1")
	assert.NotZero(t, projectID)
	require.NoError(t, manager.Reserve("sandbox-1", "runc", 4096, 8192))
	require.ErrorContains(t, manager.Reserve("sandbox-1", "runc", 4096, 16384), "different writable reservation")

	reloaded := &writableCapacityManager{
		dir:          manager.dir,
		reservations: make(map[string]writableReservation),
	}
	require.NoError(t, reloaded.load())
	assert.Equal(t, projectID, reloaded.ProjectID("sandbox-1"))
	assert.Equal(t, int64(4096), reloaded.reservations["sandbox-1"].RequestBytes)

	require.NoError(t, reloaded.Release("sandbox-1"))
	_, err := os.Stat(filepath.Join(manager.dir, "sandbox-1.json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, reloaded.Release("sandbox-1"))
}

func TestWritableCapacityReservationRejectsUnsafeContainerID(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)

	require.ErrorContains(t, manager.Reserve("../escape", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Reserve("", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Reserve("escape;command", "runsc", 1, 1), "invalid container ID")
	require.ErrorContains(t, manager.Release("../escape"), "invalid container ID")
}

func TestWritableCapacityReservationRejectsFilenameMismatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reservations")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "safe.json"), []byte(`{
  "container_id": "../escape",
  "runtime_name": "runsc",
  "request_bytes": 1,
  "limit_bytes": 1
}`), 0600))

	manager := &writableCapacityManager{dir: dir, reservations: make(map[string]writableReservation)}
	require.ErrorContains(t, manager.load(), "invalid writable reservation")
}

func TestWritableCapacityReservationEnforcesLiveAvailableFloor(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 1<<62)

	require.ErrorContains(t, manager.Reserve("sandbox-1", "runsc", 1, 1), "insufficient writable layer capacity")
}

func TestWritableCapacityReconcileCleansOnlyStaleRuntimeReservations(t *testing.T) {
	manager := newTestWritableCapacityManager(t, 0)
	require.NoError(t, manager.Reserve("active-runc", "runc", 1, 1))
	require.NoError(t, manager.Reserve("stale-runc", "runc", 1, 1))
	require.NoError(t, manager.Reserve("stale-runsc", "runsc", 1, 1))
	cleaned := make([]string, 0)

	require.NoError(t, manager.ReconcileRuntime("runc", map[string]struct{}{"active-runc": {}}, func(id string) error {
		cleaned = append(cleaned, id)
		return nil
	}))
	assert.Equal(t, []string{"stale-runc"}, cleaned)
	assert.Contains(t, manager.reservations, "active-runc")
	assert.NotContains(t, manager.reservations, "stale-runc")
	assert.Contains(t, manager.reservations, "stale-runsc")
}
