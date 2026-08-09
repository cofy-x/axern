package container

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
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
		monitorStopChan:  cmap.New[chan struct{}](),
		resourceManagers: cmap.New[resources.Manager](),
		idGenerator:      truncindex.NewTruncGenerator(config.SandboxContainerPrefix, nil),
	}
}

func TestReconcileRuntimeInventoryRemovesPersistedOrphan(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	manager.containers.Set("orphan", &Container{
		Metadata: &apipb.ContainerMetadata{ID: "orphan", RuntimeHandler: "runsc"},
		Spec:     &spec.Spec{},
	})

	require.NoError(t, manager.ReconcileRuntimeInventory(map[string]map[string]struct{}{
		"runsc": {},
	}))
	assert.False(t, manager.containers.Has("orphan"))
}

func TestReconcileRuntimeInventoryValidatesBeforeCleanup(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	manager.containers.Set("orphan", &Container{
		Metadata: &apipb.ContainerMetadata{ID: "orphan", RuntimeHandler: "runsc"},
		Spec:     &spec.Spec{},
	})

	err := manager.ReconcileRuntimeInventory(map[string]map[string]struct{}{
		"runsc": {"missing-metadata": {}},
	})
	require.ErrorContains(t, err, "has no persisted metadata")
	assert.True(t, manager.containers.Has("orphan"), "validation failure must precede destructive cleanup")
}

func TestReconcileRuntimeInventoryRejectsUnavailableRuntimeOwnership(t *testing.T) {
	manager := newRuntimeInventoryTestManager(t)
	manager.containers.Set("ambiguous", &Container{
		Metadata: &apipb.ContainerMetadata{ID: "ambiguous", RuntimeHandler: "disabled-runtime"},
		Spec:     &spec.Spec{},
	})

	err := manager.ReconcileRuntimeInventory(map[string]map[string]struct{}{
		"runsc": {},
	})
	require.ErrorContains(t, err, "inventory is unavailable")
	assert.True(t, manager.containers.Has("ambiguous"))
}
