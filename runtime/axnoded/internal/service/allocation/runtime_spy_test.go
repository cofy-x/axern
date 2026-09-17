package allocation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type runtimeSpyHandler struct {
	name               string
	requirements       contract.HostRequirements
	waitExitCode       int
	waitFunc           func(context.Context, contract.HandlerOptions) (contract.Exit, error)
	stopFunc           func(context.Context, contract.HandlerOptions) (contract.Exit, error)
	stopCalls          int
	createCalls        int
	deleteCalls        int
	lastOptions        contract.HandlerOptions
	lastDeleteOptions  contract.HandlerOptions
	lastRequest        *apipb.CreateContainerRequest
	deleteOptionCalls  []contract.HandlerOptions
	deleteErrors       []error
	createError        error
	bundleDuration     time.Duration
	launchDuration     time.Duration
	listStates         []*contract.UnionContainerState
	listError          error
	listHook           func()
	containerSpec      *specs.Spec
	containerSpecError error
	createHook         func()
	fileService        contract.FileService
}

var _ contract.SandboxRuntime = (*runtimeSpyHandler)(nil)

func (h *runtimeSpyHandler) Name() string { return h.name }

func (h *runtimeSpyHandler) HostRequirements() contract.HostRequirements { return h.requirements }

func (h *runtimeSpyHandler) Version(context.Context) (*apipb.RuntimeVersion, error) {
	return &apipb.RuntimeVersion{Version: "test"}, nil
}

func (h *runtimeSpyHandler) PrepareContainer(_ context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.PreparedContainer, error) {
	h.createCalls++
	h.lastOptions = options
	h.lastRequest = request
	if h.createHook != nil {
		h.createHook()
	}
	options.RecordStartupPhase(contract.StartupPhaseRuntimeBundle, h.bundleDuration)
	options.RecordStartupPhase(contract.StartupPhaseRuntimeLaunch, h.launchDuration)
	if h.createError != nil {
		return nil, h.createError
	}
	metadata := &apipb.ContainerMetadata{
		Stdout: request.Stdout,
		Stderr: request.Stderr,
	}
	return &contract.PreparedContainer{ContainerID: options.ContainerID, BundlePath: "/fake/" + options.ContainerID, Metadata: metadata}, nil
}

func (h *runtimeSpyHandler) StartPreparedContainer(_ context.Context, prepared *contract.PreparedContainer, _ contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return prepared.Metadata, nil
}

func (h *runtimeSpyHandler) AllocationEnforcementManifest(_ context.Context, containerID string) (*apipb.AllocationEnforcementManifest, error) {
	return &apipb.AllocationEnforcementManifest{
		BundlePath:        "/fake/" + containerID,
		CreatedAtUnixNano: time.Now().UTC().UnixNano(),
	}, nil
}

func (h *runtimeSpyHandler) DeleteContainer(_ context.Context, _ *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	h.deleteCalls++
	h.lastDeleteOptions = options
	h.deleteOptionCalls = append(h.deleteOptionCalls, options)
	if len(h.deleteErrors) > 0 {
		err := h.deleteErrors[0]
		h.deleteErrors = h.deleteErrors[1:]
		return &apipb.DeleteContainerResponse{}, err
	}
	return &apipb.DeleteContainerResponse{}, nil
}

func (h *runtimeSpyHandler) KillContainer(context.Context, *apipb.SignalContainerRequest, contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	return &apipb.SignalContainerResponse{}, nil
}

func (h *runtimeSpyHandler) StopWorkload(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	h.stopCalls++
	if h.stopFunc != nil {
		return h.stopFunc(ctx, options)
	}
	return contract.Exit{Status: 137, Timestamp: time.Now().UTC()}, nil
}

func (h *runtimeSpyHandler) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	if h.listHook != nil {
		h.listHook()
	}
	if h.listStates != nil || h.listError != nil {
		return h.listStates, h.listError
	}
	return []*contract.UnionContainerState{}, nil
}

func (h *runtimeSpyHandler) ContainerSpec(context.Context, contract.HandlerOptions) (*specs.Spec, error) {
	if h.containerSpec != nil || h.containerSpecError != nil {
		return h.containerSpec, h.containerSpecError
	}
	return &specs.Spec{}, nil
}

func (h *runtimeSpyHandler) ExecContainer(context.Context, *apipb.ExecContainerRequest, contract.HandlerOptions) (*apipb.ExecContainerResponse, error) {
	return &apipb.ExecContainerResponse{}, nil
}

func (h *runtimeSpyHandler) OpenExecSession(context.Context, *apipb.ExecSessionOpen, contract.HandlerOptions) (contract.Session, error) {
	return nil, nil
}

func (h *runtimeSpyHandler) FileService() contract.FileService { return h.fileService }

func (h *runtimeSpyHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	if h.waitFunc != nil {
		return h.waitFunc(ctx, options)
	}
	return contract.Exit{Status: h.waitExitCode}, nil
}

func (h *runtimeSpyHandler) ShutDown() {}

func writeContainerSpecFile(t *testing.T, rootDir, containerID string, annotations map[string]string) {
	t.Helper()
	containerDir := filepath.Join(rootDir, "containers", containerID)
	if err := os.MkdirAll(containerDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	spec := specs.Spec{Version: "1.0.0", Annotations: annotations}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal spec error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), data, 0644); err != nil {
		t.Fatalf("WriteFile spec error = %v", err)
	}
}
