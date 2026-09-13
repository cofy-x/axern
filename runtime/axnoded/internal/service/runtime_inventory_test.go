package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inventoryTestHandler struct {
	contract.SandboxRuntime
	states    []*contract.UnionContainerState
	err       error
	deleted   *[]string
	deleteErr error
	waitExit  contract.Exit
	waitErr   error
}

func (h inventoryTestHandler) Wait(context.Context, contract.HandlerOptions) (contract.Exit, error) {
	return h.waitExit, h.waitErr
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

func runtimeInventoryTestService(t *testing.T, handler contract.SandboxRuntime) *sandboxService {
	t.Helper()
	manager, err := container.NewManager(t.TempDir(), handler, make(chan bool, 1))
	require.NoError(t, err)
	return &sandboxService{containerManager: manager, runscHandler: handler}
}

func TestCollectRuntimeInventoryRequiresCompleteGeneration(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{SandboxRuntime: runsc, err: errors.New("runsc unavailable")})

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "list runsc containers")
	assert.Nil(t, inventory)
}

func TestCollectRuntimeInventoryReturnsAllocationView(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{SandboxRuntime: runsc, states: []*contract.UnionContainerState{{ID: "live", Status: contract.ContainerStatusRunning}}})

	inventory, err := service.collectRuntimeInventory(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"live": {}}, inventory.allIDs())
}

func TestRuntimeInventoryRetainsUnknownAndExcludesTerminalAfterRuntimeDelete(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	deleted := make([]string, 0)
	service := runtimeInventoryTestService(t, inventoryTestHandler{
		SandboxRuntime: runsc,
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

func TestRecoverTerminalRuntimeCheckpointBeforeCleanup(t *testing.T) {
	exitedAt := time.Date(2026, 9, 13, 12, 0, 0, 123, time.UTC)
	runsc := runtimetest.NewFakeSandboxRuntime()
	handler := inventoryTestHandler{SandboxRuntime: runsc, waitExit: contract.Exit{Status: 23, Timestamp: exitedAt}}
	service := runtimeInventoryTestService(t, handler)
	require.NoError(t, service.containerManager.StoreMetadata("terminal", &runtimeapi.ContainerMetadata{}))

	require.NoError(t, service.recoverTerminalRuntimeCheckpoints(context.Background(), runtimeInventory{
		"terminal": {ID: "terminal", Status: contract.ContainerStatusExited},
	}))
	item, err := service.containerManager.Get("terminal")
	require.NoError(t, err)
	status := item.Status.Get()
	assert.Equal(t, runtimeapi.ContainerState_CONTAINER_EXITED, status.State())
	assert.Equal(t, int32(23), status.ExitCode)
	assert.True(t, status.ExitCodeKnown)
	assert.Equal(t, exitedAt, container.ParseTimestampTime(status.FinishedAt))
}

func TestRecoverTerminalRuntimeCheckpointFailsClosedWithoutExactExit(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	handler := inventoryTestHandler{SandboxRuntime: runsc, waitErr: contract.ErrExitStatusUnavailable}
	service := runtimeInventoryTestService(t, handler)
	require.NoError(t, service.containerManager.StoreMetadata("terminal", &runtimeapi.ContainerMetadata{}))

	err := service.recoverTerminalRuntimeCheckpoints(context.Background(), runtimeInventory{
		"terminal": {ID: "terminal", Status: contract.ContainerStatusExited},
	})
	require.ErrorIs(t, err, contract.ErrExitStatusUnavailable)
	item, getErr := service.containerManager.Get("terminal")
	require.NoError(t, getErr)
	assert.Equal(t, runtimeapi.ContainerState_CONTAINER_UNKNOWN, item.Status.Get().State())
}

func TestCollectRuntimeInventoryRejectsInvalidStatus(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, inventoryTestHandler{
		SandboxRuntime: runsc,
		states:         []*contract.UnionContainerState{{ID: "bad", Status: "paused"}},
	})

	_, err := service.collectRuntimeInventory(context.Background())
	require.ErrorContains(t, err, "invalid status")
}

func TestInterruptedStartRecoveryAction(t *testing.T) {
	tests := []struct {
		name           string
		live           bool
		status         contract.ContainerStatus
		launchVerified bool
		wantCleanup    bool
		wantError      bool
	}{
		{name: "intent without runtime", wantCleanup: true},
		{name: "prepared verified runtime", live: true, status: contract.ContainerStatusCreated, launchVerified: true, wantCleanup: true},
		{name: "prepared unverified runtime", live: true, status: contract.ContainerStatusCreated, wantCleanup: true},
		{name: "verified running runtime", live: true, status: contract.ContainerStatusRunning, launchVerified: true},
		{name: "verified terminal runtime", live: true, status: contract.ContainerStatusExited, launchVerified: true},
		{name: "unverified terminal runtime", live: true, status: contract.ContainerStatusExited, wantCleanup: true},
		{name: "unverified running runtime", live: true, status: contract.ContainerStatusRunning, wantError: true},
		{name: "unverified unknown runtime", live: true, status: contract.ContainerStatusUnknown, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup, err := interruptedStartRecoveryAction(tt.live, tt.status, tt.launchVerified)
			assert.Equal(t, tt.wantCleanup, cleanup)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCleanupInterruptedAllocationStartWithoutRuntime(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	service := newTestService(t, runsc)
	controller := service.allocationController()
	const allocationID = "interrupted-before-oci-create"
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, controller.BindControlPlaneAllocation(allocationID, "node-a", digest))
	require.NoError(t, controller.StoreAllocationIntent(allocationID, digest, nil, nil))
	recovery, err := controller.InspectRecoveryRecords()
	require.NoError(t, err)

	inventory := runtimeInventory{}
	require.NoError(t, service.cleanupInterruptedAllocationStarts(context.Background(), inventory, recovery))
	assert.Empty(t, inventory)
	assert.Empty(t, recovery.Intents)
	assert.Empty(t, recovery.LaunchVerified)
	assert.False(t, controller.HasAllocation(allocationID))
	assert.True(t, controller.HasControlPlaneBinding(allocationID), "control-plane admission must survive a retryable node start interruption")
	after, err := controller.InspectRecoveryRecords()
	require.NoError(t, err)
	assert.Empty(t, after.Intents)
}

func TestCleanupInterruptedAllocationStartDeletesOrphanedRecoveryRecord(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	service := newTestService(t, runsc)
	controller := service.allocationController()
	const allocationID = "orphaned-create-intent"
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	record := &runtimeapi.AllocationState{AllocationID: allocationID, AllocationRequestDigest: digest}
	require.NoError(t, service.store.PutRecord(config.AllocationStateBucket, allocationID, record))
	recovery, err := controller.InspectRecoveryRecords()
	require.NoError(t, err)

	require.NoError(t, service.cleanupInterruptedAllocationStarts(context.Background(), runtimeInventory{}, recovery))
	after, err := controller.InspectRecoveryRecords()
	require.NoError(t, err)
	assert.Empty(t, after.Intents)
	assert.False(t, controller.HasControlPlaneBinding(allocationID))
}

func TestPartitionRuntimeInventoryRequiresExplicitConsistentRecoveryAuthority(t *testing.T) {
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, runsc)
	require.NoError(t, service.containerManager.StoreMetadata("durable", &runtimeapi.ContainerMetadata{
		RecoveryMode: runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DURABLE,
	}))
	require.NoError(t, service.containerManager.StoreMetadata("session", &runtimeapi.ContainerMetadata{
		RecoveryMode: runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DISCARD_ON_RESTART,
	}))
	inventory := runtimeInventory{
		"durable": {ID: "durable", Status: contract.ContainerStatusRunning},
		"session": {ID: "session", Status: contract.ContainerStatusRunning},
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
	runsc := runtimetest.NewFakeSandboxRuntime()
	runsc.RuntimeName = "runsc"
	service := runtimeInventoryTestService(t, runsc)
	require.NoError(t, service.containerManager.StoreMetadata("ambiguous", &runtimeapi.ContainerMetadata{}))

	_, _, err := service.partitionRuntimeInventory(
		runtimeInventory{"ambiguous": {ID: "ambiguous", Status: contract.ContainerStatusRunning}},
		map[string]struct{}{"ambiguous": {}},
		map[string]struct{}{"ambiguous": {}},
	)
	require.ErrorContains(t, err, "no explicit recovery mode")
}
