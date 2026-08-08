package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRuncPrepareCreateRequestClearsCgroupOptionsWhenIgnored(t *testing.T) {
	handler := &RuncServiceHandler{ignoreCgroups: true}
	request := &apipb.CreateContainerRequest{
		Resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024},
		Rootfs:   &apipb.Rootfs{Readonly: true},
	}

	effective, options, err := handler.prepareCreateRequest(request, contract.HandlerOptions{
		CgroupPath:        "/sandbox/test",
		RuntimeCgroupPath: "/sandbox/test/workload",
	})

	assert.NoError(t, err)
	assert.NotSame(t, request, effective)
	assert.Nil(t, effective.Resource)
	assert.Empty(t, options.CgroupPath)
	assert.Empty(t, options.RuntimeCgroupPath)
}

func TestRuncPrepareCreateRequestRejectsMissingRequiredCgroup(t *testing.T) {
	request := &apipb.CreateContainerRequest{
		Resource: &runtimeapi.LinuxContainerResources{CpuShares: 1024},
		Rootfs:   &apipb.Rootfs{Readonly: true},
	}

	effective, _, err := (&RuncServiceHandler{}).prepareCreateRequest(request, contract.HandlerOptions{})

	assert.ErrorContains(t, err, "no sandbox cgroup was allocated")
	assert.Nil(t, effective)
}

func TestRuncCreateContainerRejectsMemoryLimitWhenCgroupsDisabled(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{
		Binary: writeFakeOCIRuntimeBinary(t, rootDir, "runc"),
	}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.common.SetRuntimeRunnerBinary(writeFakeRuntimeRunnerBinary(t, rootDir))
	disableSandboxReadyWait(t, handler)
	handler.ignoreCgroups = true

	request := newLocalCreateRequest(t)
	request.Runtime = config.RuntimeNameRunc
	request.Resource = &runtimeapi.LinuxContainerResources{
		MemoryLimitInBytes: 1024,
	}

	_, err = handler.CreateContainer(context.Background(), request, contract.HandlerOptions{
		ContainerID: "runc-ignore-cgroups",
		CgroupPath:  "/sandbox/test",
		RootfsType:  contract.StartupRootfsTypeLocal,
	})
	assert.ErrorContains(t, err, "memory limit requires cgroup enforcement")
}
