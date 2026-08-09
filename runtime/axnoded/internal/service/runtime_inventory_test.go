package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inventoryTestHandler struct {
	contract.RuntimeHandler
	states []*contract.UnionContainerState
	err    error
}

func (h inventoryTestHandler) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return h.states, h.err
}

func runtimeInventoryTestService(t *testing.T, handlers ...contract.RuntimeHandler) *sandboxService {
	t.Helper()
	registered := cmap.New[contract.RuntimeHandler]()
	for _, handler := range handlers {
		registered.Set(handler.Name(), handler)
	}
	manager, err := container.NewManager(t.TempDir(), registered, make(chan bool, 1))
	require.NoError(t, err)
	return &sandboxService{containerManager: manager}
}

func TestCollectRuntimeInventoryRequiresCompleteGeneration(t *testing.T) {
	runc := runtimetest.NewFakeRuntimeHandler()
	runc.RuntimeName = "runc"
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t,
		inventoryTestHandler{RuntimeHandler: runc, states: []*contract.UnionContainerState{{ID: "runc-live"}}},
		inventoryTestHandler{RuntimeHandler: runsc, err: errors.New("runsc unavailable")},
	)

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "list runsc containers")
	assert.Nil(t, inventory)
}

func TestCollectRuntimeInventoryRejectsDuplicateOwnership(t *testing.T) {
	runc := runtimetest.NewFakeRuntimeHandler()
	runc.RuntimeName = "runc"
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t,
		inventoryTestHandler{RuntimeHandler: runc, states: []*contract.UnionContainerState{{ID: "duplicate"}}},
		inventoryTestHandler{RuntimeHandler: runsc, states: []*contract.UnionContainerState{{ID: "duplicate"}}},
	)

	_, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "reported by both")
}

func TestCollectRuntimeInventoryReturnsRuntimeScopedAndGlobalViews(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t,
		inventoryTestHandler{RuntimeHandler: runsc, states: []*contract.UnionContainerState{{ID: "live"}}},
	)

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"live": {}}, inventory.forRuntime("runsc"))
	assert.Equal(t, map[string]struct{}{"live": {}}, inventory.allIDs())
}
