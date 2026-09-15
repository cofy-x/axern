package allocation

import (
	"context"
	"fmt"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDeleteContainerWithRuntimeForceDelete(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)

	resp, err := fixture.controller.deleteContainerWithRuntime(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-force",
		Timeout: 0,
	}, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.deleteCalls)
	assert.True(t, handler.lastDeleteOptions.ForceDelete)
}

func TestDeleteContainerWithRuntimeTimeoutFallsBackToForceDelete(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{fmt.Errorf("boom")},
	}
	fixture := newTestAllocationController(t, handler)

	resp, err := fixture.controller.deleteContainerWithRuntime(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-fallback",
		Timeout: 1,
	}, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 2, handler.deleteCalls)
	assert.Len(t, handler.deleteOptionCalls, 2)
	assert.False(t, handler.deleteOptionCalls[0].ForceDelete)
	assert.True(t, handler.deleteOptionCalls[1].ForceDelete)
}

func TestDeleteContainerWithRuntimeNotFoundIsIdempotent(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{status.Error(codes.NotFound, "not found")},
	}
	fixture := newTestAllocationController(t, handler)
	resp, err := fixture.controller.deleteContainerWithRuntime(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-runtime-not-found",
		Timeout: 0,
	}, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.deleteCalls)
}

func TestDeleteAllocationRemovesRuntimeReferenceOnSuccess(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	containerID := "axctl-delete-allocation-success"
	storeTestContainer(t, fixture, containerID, "runsc")
	lrt := addTestRuntimeMappingRuntime(t, fixture.environmentCache, testResolvedEnvironment(t, "rt-1"))
	lrt.IncRef()
	assert.NoError(t, fixture.controller.rememberContainerRuntime(containerID, lrt))

	resp, err := fixture.controller.Delete(context.Background(), &runtime.DeleteRequest{ID: containerID})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.deleteCalls)
	_, ok := fixture.controller.runtimeMapping(containerID)
	assert.False(t, ok)
	_, getErr := fixture.manager.Get(containerID)
	assert.Error(t, getErr)
}

func TestDeleteAllocationPreservesRuntimeReferenceOnFailure(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{fmt.Errorf("boom")},
	}
	fixture := newTestAllocationController(t, handler)
	containerID := "axctl-delete-allocation-failure"
	storeTestContainer(t, fixture, containerID, "runsc")
	lrt := addTestRuntimeMappingRuntime(t, fixture.environmentCache, testResolvedEnvironment(t, "rt-1"))
	lrt.IncRef()
	assert.NoError(t, fixture.controller.rememberContainerRuntime(containerID, lrt))

	resp, err := fixture.controller.Delete(context.Background(), &runtime.DeleteRequest{ID: containerID})

	assert.Error(t, err)
	assert.NotNil(t, resp)
	_, ok := fixture.controller.runtimeMapping(containerID)
	assert.True(t, ok)
}

func storeTestContainer(t *testing.T, fixture testAllocationController, containerID string, runtimeName string) {
	t.Helper()
	writeContainerSpecFile(t, fixture.controller.config.RootDir, containerID, nil)
	metadata := &apipb.ContainerMetadata{}
	assert.NoError(t, fixture.manager.StoreMetadata(containerID, metadata))
	assert.NoError(t, fixture.manager.StartMonitor(containerID, metadata))
	assert.Eventually(t, func() bool {
		stored, err := fixture.manager.Get(containerID)
		return err == nil && stored.Status.Get().State() == apipb.ContainerState_CONTAINER_EXITED
	}, time.Second, time.Millisecond, "runtime spy monitor did not persist its immediate exit")
}
