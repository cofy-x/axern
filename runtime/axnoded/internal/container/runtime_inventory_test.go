package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRuntimeInventoryTestManager(t *testing.T) *Manager {
	t.Helper()
	return &Manager{
		root:             t.TempDir(),
		recyclePath:      t.TempDir(),
		containers:       cmap.New[*Container](),
		monitors:         cmap.New[*containerMonitor](),
		resourceManagers: cmap.New[resources.Manager](),
	}
}

func TestReconcileRuntimeInventoryRemovesPersistedOrphan(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	require.NoError(t, manager.StoreMetadata("orphan", &apipb.ContainerMetadata{}))

	require.NoError(t, manager.ReconcileRuntimeInventory(map[string]struct{}{}))
	assert.False(t, manager.containers.Has("orphan"))
}

func TestReconcileRuntimeInventoryRemovesDiskOrphanWithoutMetadata(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	orphanRoot := filepath.Join(manager.root, "alloc-terminal")
	require.NoError(t, os.MkdirAll(orphanRoot, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(orphanRoot, config.ContainerSpecFile),
		[]byte(`{"ociVersion":"1.2.0"}`),
		0o600,
	))

	require.NoError(t, manager.ReconcileRuntimeInventory(map[string]struct{}{}))
	_, err := os.Stat(orphanRoot)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReconcileRuntimeInventoryRemovesProvenEmptyDiskOrphan(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	orphanRoot := filepath.Join(manager.root, "alloc-empty-terminal")
	require.NoError(t, os.MkdirAll(orphanRoot, 0o755))

	require.NoError(t, manager.ReconcileRuntimeInventory(map[string]struct{}{}))
	_, err := os.Stat(orphanRoot)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReconcileRuntimeInventoryValidatesBeforeCleanup(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	manager.containers.Set("orphan", &Container{
		Metadata: &apipb.ContainerMetadata{},
		Spec:     &spec.Spec{},
	})

	err := manager.ReconcileRuntimeInventory(map[string]struct{}{"missing-metadata": {}})
	require.ErrorContains(t, err, "has no persisted metadata")
	assert.True(t, manager.containers.Has("orphan"), "validation failure must precede destructive cleanup")
}
