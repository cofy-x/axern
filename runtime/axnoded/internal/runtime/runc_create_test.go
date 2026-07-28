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
		Resource: &runtimeapi.LinuxContainerResources{MemoryLimitInBytes: 1024},
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

func TestRuncPrepareCreateRequestKeepsEmptyCgroupPathWhenCgroupsEnabled(t *testing.T) {
	request := &apipb.CreateContainerRequest{
		Resource: &runtimeapi.LinuxContainerResources{MemoryLimitInBytes: 1024},
	}

	effective, options, err := (&RuncServiceHandler{}).prepareCreateRequest(request, contract.HandlerOptions{})

	assert.NoError(t, err)
	assert.Same(t, request, effective)
	assert.Empty(t, options.CgroupPath)
	assert.Empty(t, options.RuntimeCgroupPath)
}

func TestRuncCreateContainerSkipsCgroupUpdateWhenIgnoreCgroupsEnabled(t *testing.T) {
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
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	bundlePath := filepath.Join(rootDir, "containers", "runc-ignore-cgroups")
	ociSpec, err := runtimeoci.LoadSpec(filepath.Join(bundlePath, config.ContainerSpecFile))
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if ociSpec.Linux == nil {
		t.Fatalf("expected linux section in spec")
	}
	assert.Empty(t, ociSpec.Linux.CgroupsPath)
}
