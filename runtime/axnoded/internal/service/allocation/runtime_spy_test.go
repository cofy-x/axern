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
	name                 string
	capabilities         contract.RuntimeCapabilities
	requirements         contract.RuntimeRequirements
	waitExitCode         int
	createCalls          int
	deleteCalls          int
	lastOptions          contract.HandlerOptions
	lastDeleteOptions    contract.HandlerOptions
	lastRequest          *apipb.CreateContainerRequest
	deleteOptionCalls    []contract.HandlerOptions
	deleteErrors         []error
	createError          error
	bundleDuration       time.Duration
	launchDuration       time.Duration
	listStates           []*contract.UnionContainerState
	listError            error
	containerSpec        *specs.Spec
	containerSpecError   error
	createMetadataLabels map[string]string
	createHook           func()
}

var _ contract.RuntimeHandler = (*runtimeSpyHandler)(nil)

func (h *runtimeSpyHandler) Name() string { return h.name }

func (h *runtimeSpyHandler) Capabilities() contract.RuntimeCapabilities { return h.capabilities }

func (h *runtimeSpyHandler) Requirements() contract.RuntimeRequirements { return h.requirements }

func (h *runtimeSpyHandler) Version(context.Context) (*apipb.RuntimeVersion, error) {
	return &apipb.RuntimeVersion{RuntimeName: h.name, RuntimeVersion: "test"}, nil
}

func (h *runtimeSpyHandler) CreateContainer(_ context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
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
	labels := map[string]string{}
	for k, v := range request.GetLabels() {
		labels[k] = v
	}
	for k, v := range options.AdditionalAnnotations {
		labels[k] = v
	}
	for k, v := range h.createMetadataLabels {
		labels[k] = v
	}
	return &apipb.ContainerMetadata{
		ID:             options.ContainerID,
		RuntimeHandler: h.name,
		Labels:         labels,
		Stdout:         request.Stdout,
		Stderr:         request.Stderr,
	}, nil
}

func (h *runtimeSpyHandler) PrepareContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.PreparedContainer, error) {
	metadata, err := h.CreateContainer(ctx, request, options)
	if err != nil {
		return nil, err
	}
	return &contract.PreparedContainer{ContainerID: options.ContainerID, BundlePath: "/fake/" + options.ContainerID, Metadata: metadata}, nil
}

func (h *runtimeSpyHandler) StartPreparedContainer(_ context.Context, prepared *contract.PreparedContainer, _ contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return prepared.Metadata, nil
}

func (h *runtimeSpyHandler) AllocationEnforcementManifest(_ context.Context, containerID string) (*apipb.AllocationEnforcementManifest, error) {
	return &apipb.AllocationEnforcementManifest{
		RuntimeName:       h.Name(),
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

func (h *runtimeSpyHandler) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
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

func (h *runtimeSpyHandler) ProcessService() contract.ProcessService { return nil }

func (h *runtimeSpyHandler) FileService() contract.FileService { return nil }

func (h *runtimeSpyHandler) CheckpointContainer(*apipb.CheckpointRequest) error { return nil }

func (h *runtimeSpyHandler) Wait(context.Context, contract.HandlerOptions) (contract.Exit, error) {
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
