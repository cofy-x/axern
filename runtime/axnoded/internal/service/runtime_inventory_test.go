package service

import (
	"context"
	"errors"
	"testing"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inventoryTestHandler struct {
	contract.RuntimeHandler
	states    []*contract.UnionContainerState
	err       error
	deleted   *[]string
	deleteErr error
}

func (h inventoryTestHandler) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return h.states, h.err
}

func (h inventoryTestHandler) DeleteContainer(_ context.Context, _ *runtimeapi.DeleteContainerRequest, options contract.HandlerOptions) (*runtimeapi.DeleteContainerResponse, error) {
	if h.deleted != nil {
		*h.deleted = append(*h.deleted, options.ContainerID)
	}
	return &runtimeapi.DeleteContainerResponse{}, h.deleteErr
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
		inventoryTestHandler{RuntimeHandler: runc, states: []*contract.UnionContainerState{{ID: "runc-live", Status: contract.ContainerStatusRunning}}},
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
		inventoryTestHandler{RuntimeHandler: runc, states: []*contract.UnionContainerState{{ID: "duplicate", Status: contract.ContainerStatusRunning}}},
		inventoryTestHandler{RuntimeHandler: runsc, states: []*contract.UnionContainerState{{ID: "duplicate", Status: contract.ContainerStatusRunning}}},
	)

	_, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "reported by both")
}

func TestCollectRuntimeInventoryReturnsRuntimeScopedAndGlobalViews(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t,
		inventoryTestHandler{RuntimeHandler: runsc, states: []*contract.UnionContainerState{{ID: "live", Status: contract.ContainerStatusRunning}}},
	)

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"live": {}}, inventory.forRuntime("runsc"))
	assert.Equal(t, map[string]struct{}{"live": {}}, inventory.allIDs())
}

func TestRuntimeInventoryRetainsUnknownAndExcludesTerminalAfterRuntimeDelete(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	deleted := make([]string, 0)
	service := runtimeInventoryTestService(t, inventoryTestHandler{
		RuntimeHandler: runsc,
		states: []*contract.UnionContainerState{
			{ID: "terminal", Status: contract.ContainerStatusExited},
			{ID: "unknown", Status: contract.ContainerStatusUnknown},
		},
		deleted: &deleted,
	})
	inventory, err := service.collectRuntimeInventory(context.Background())
	require.NoError(t, err)

	require.NoError(t, service.cleanupTerminalRuntimeContainers(context.Background(), inventory))
	assert.Equal(t, []string{"terminal"}, deleted)
	assert.Equal(t, map[string]struct{}{"unknown": {}}, inventory.retained().forRuntime("runsc"))
}

func TestCollectRuntimeInventoryRejectsInvalidStatus(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{
		RuntimeHandler: runsc,
		states:         []*contract.UnionContainerState{{ID: "bad", Status: "paused"}},
	})

	_, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "invalid status")
}
