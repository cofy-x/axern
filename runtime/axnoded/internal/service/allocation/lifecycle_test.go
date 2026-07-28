package allocation

import (
	"context"
	"fmt"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateRuntimeContainerUsesRuntimeRequirements(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runtime-requirements-test",
		requirements: contract.RuntimeRequirements{},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{
		"runtime-requirements-test": handler,
	})

	resp, _, err := fixture.controller.CreateRuntimeContainer(context.Background(), nil, nil, &apipb.CreateContainerRequest{
		Runtime: "runtime-requirements-test",
		Rootfs: &apipb.Rootfs{
			RootDir:  t.TempDir(),
			Readonly: false,
		},
		Command: []string{"/bin/true"},
	}, nil, nil)

	assert.NoError(t, err)
	assert.NotEmpty(t, resp.GetID())
	assert.Equal(t, 1, handler.createCalls)
	assert.Empty(t, handler.lastOptions.AllocatedResources)
}

func TestDeleteRuntimeContainerWithHandlerForceDeleteUsesCleanRootDir(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	target := testDeleteTarget("axctl-delete-force", "/tmp/task-123/workdir", "TASK_UUID=task-123", map[string]string{"key": "value"})

	resp, err := fixture.controller.DeleteRuntimeContainerWithHandler(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-force",
		Timeout: 0,
	}, target, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.deleteCalls)
	assert.True(t, handler.lastDeleteOptions.ForceDelete)
	assert.Equal(t, "/tmp/task-123/workdir", handler.lastDeleteOptions.CleanRootDir)
	assert.Equal(t, map[string]string{"key": "value"}, handler.lastDeleteOptions.AdditionalAnnotations)
}

func TestDeleteRuntimeContainerWithHandlerTimeoutFallsBackToForceDelete(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{fmt.Errorf("boom")},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	target := testDeleteTarget("axctl-delete-fallback", "/tmp/task-456/workdir", "TASK_UUID=task-456", map[string]string{"key": "value"})

	resp, err := fixture.controller.DeleteRuntimeContainerWithHandler(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-fallback",
		Timeout: 1,
	}, target, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 2, handler.deleteCalls)
	assert.Len(t, handler.deleteOptionCalls, 2)
	assert.False(t, handler.deleteOptionCalls[0].ForceDelete)
	assert.Equal(t, "/tmp/task-456/workdir", handler.deleteOptionCalls[0].CleanRootDir)
	assert.True(t, handler.deleteOptionCalls[1].ForceDelete)
	assert.Empty(t, handler.deleteOptionCalls[1].CleanRootDir)
}

func TestDeleteRuntimeContainerWithHandlerRuntimeNotFoundIsIdempotent(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{status.Error(codes.NotFound, "not found")},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	target := &container.Container{
		Metadata: &apipb.ContainerMetadata{
			ID:             "axctl-delete-runtime-not-found",
			RuntimeHandler: "runsc",
		},
		Spec: &specs.Spec{Annotations: map[string]string{}},
	}

	resp, err := fixture.controller.DeleteRuntimeContainerWithHandler(context.Background(), &apipb.DeleteContainerRequest{
		ID:      "axctl-delete-runtime-not-found",
		Timeout: 0,
	}, target, handler, "trace-id", "span-id")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, handler.deleteCalls)
}

func TestDeleteManagedContainerRemovesRuntimeReferenceOnSuccess(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	containerID := "axctl-delete-managed-success"
	storeTestContainer(t, fixture, containerID, "runsc")
	lrt := addTestRuntimeMappingRuntime(t, fixture.lrtManager, testRuntimeTemplate(t, "rt-1"))
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

func TestDeleteManagedContainerPreservesRuntimeReferenceOnFailure(t *testing.T) {
	handler := &runtimeSpyHandler{
		name:         "runsc",
		deleteErrors: []error{fmt.Errorf("boom")},
	}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	containerID := "axctl-delete-managed-failure"
	storeTestContainer(t, fixture, containerID, "runsc")
	lrt := addTestRuntimeMappingRuntime(t, fixture.lrtManager, testRuntimeTemplate(t, "rt-1"))
	lrt.IncRef()
	assert.NoError(t, fixture.controller.rememberContainerRuntime(containerID, lrt))

	resp, err := fixture.controller.Delete(context.Background(), &runtime.DeleteRequest{ID: containerID})

	assert.Error(t, err)
	assert.NotNil(t, resp)
	_, ok := fixture.controller.runtimeMapping(containerID)
	assert.True(t, ok)
}

func TestConfigureStartPortsNoContainerIPRollsBack(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	containerID := "axctl-start-rollback-no-ip"
	storeTestContainer(t, fixture, containerID, "runsc")

	err := fixture.controller.ConfigureStartPorts(context.Background(), containerID, "", []string{"tcp:8080:80"})

	assert.EqualError(t, err, "Failed to get container IP for DNAT")
	assert.Equal(t, 1, handler.deleteCalls)
	_, getErr := fixture.manager.Get(containerID)
	assert.Error(t, getErr)
}

func TestConfigureStartPortsDnatFailureRollsBack(t *testing.T) {
	fake := &fakeNetworkManager{failNext: true}
	networkmanager.Register(testNetworkType, fake)
	t.Cleanup(func() {
		delete(networkmanager.NetworkManagers, testNetworkType)
	})
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	fixture.controller.config.PluginConfig.NetworkConfig.NatBackend = testNetworkType
	containerID := "axctl-start-rollback-dnat"
	storeTestContainer(t, fixture, containerID, "runsc")

	err := fixture.controller.ConfigureStartPorts(context.Background(), containerID, "10.0.0.2", []string{"tcp:8080:80"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to setup DNAT rules")
	assert.Equal(t, 1, handler.deleteCalls)
	assert.Empty(t, fake.removed)
	_, getErr := fixture.manager.Get(containerID)
	assert.Error(t, getErr)
}

func testDeleteTarget(id string, cwd string, env string, annotations map[string]string) *container.Container {
	return &container.Container{
		Metadata: &apipb.ContainerMetadata{
			ID:             id,
			RuntimeHandler: "runsc",
		},
		Spec: &specs.Spec{
			Process: &specs.Process{
				Cwd: cwd,
				Env: []string{env},
			},
			Annotations: annotations,
		},
	}
}

func storeTestContainer(t *testing.T, fixture testAllocationController, containerID string, runtimeName string) {
	t.Helper()
	writeContainerSpecFile(t, fixture.controller.config.RootDir, containerID, nil)
	fixture.manager.StoreMetadata(containerID, &apipb.ContainerMetadata{
		ID:             containerID,
		RuntimeHandler: runtimeName,
	})
}
