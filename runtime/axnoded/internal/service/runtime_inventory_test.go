package service

import (
	"context"
	"errors"
	"testing"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
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

func runtimeInventoryTestService(t *testing.T, handler contract.RuntimeHandler) *sandboxService {
	t.Helper()
	manager, err := container.NewManager(t.TempDir(), handler, make(chan bool, 1))
	require.NoError(t, err)
	return &sandboxService{containerManager: manager, runscHandler: handler}
}

func TestCollectRuntimeInventoryRequiresCompleteGeneration(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{RuntimeHandler: runsc, err: errors.New("runsc unavailable")})

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "list runsc containers")
	assert.Nil(t, inventory)
}

func TestCollectRuntimeInventoryReturnsAllocationView(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{RuntimeHandler: runsc, states: []*contract.UnionContainerState{{ID: "live", Status: contract.ContainerStatusRunning}}})

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.NoError(t, err)
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
	assert.Equal(t, map[string]struct{}{"unknown": {}}, inventory.retained().allIDs())
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

func TestPartitionRuntimeInventoryRequiresExplicitConsistentRecoveryAuthority(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, runsc)
	require.NoError(t, service.containerManager.StoreMetadata("durable", &runtimeapi.ContainerMetadata{
		RecoveryMode: runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DURABLE,
	}))
	require.NoError(t, service.containerManager.StoreMetadata("session", &runtimeapi.ContainerMetadata{
		RecoveryMode: runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DISCARD_ON_RESTART,
	}))
	inventory := runtimeInventory{
		"durable": contract.ContainerStatusRunning,
		"session": contract.ContainerStatusRunning,
	}

	durable, discard, err := service.partitionRuntimeInventory(
		inventory,
		map[string]struct{}{"durable": {}},
		map[string]struct{}{"durable": {}},
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"durable": {}}, durable.allIDs())
	assert.Equal(t, map[string]struct{}{"session": {}}, discard.allIDs())

	_, _, err = service.partitionRuntimeInventory(inventory, map[string]struct{}{"durable": {}}, nil)
	require.ErrorContains(t, err, "missing AllocationState or control-plane admission binding")
	_, _, err = service.partitionRuntimeInventory(inventory, map[string]struct{}{"durable": {}}, map[string]struct{}{"durable": {}, "session": {}})
	require.ErrorContains(t, err, "discard-on-restart container session has a control-plane admission binding")
}

func TestPartitionRuntimeInventoryRejectsImplicitRecoveryMode(t *testing.T) {
	runsc := runtimetest.NewFakeRuntimeHandler()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, runsc)
	require.NoError(t, service.containerManager.StoreMetadata("ambiguous", &runtimeapi.ContainerMetadata{}))

	_, _, err := service.partitionRuntimeInventory(
		runtimeInventory{"ambiguous": contract.ContainerStatusRunning},
		map[string]struct{}{"ambiguous": {}},
		map[string]struct{}{"ambiguous": {}},
	)
	require.ErrorContains(t, err, "no explicit recovery mode")
}
