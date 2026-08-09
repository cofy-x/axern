package service

import (
	"context"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type runtimeSpyHandler struct {
	name                 string
	capabilities         contract.RuntimeCapabilities
	requirements         contract.RuntimeRequirements
	waitExitCode         int
	waitFunc             func(context.Context, contract.HandlerOptions) (contract.Exit, error)
	createCalls          int
	deleteCalls          int
	killCalls            int
	lastOptions          contract.HandlerOptions
	lastExecOptions      contract.HandlerOptions
	lastSessionOptions   contract.HandlerOptions
	lastProcessOptions   contract.HandlerOptions
	lastRequest          *apipb.CreateContainerRequest
	lastExecRequest      *apipb.ExecContainerRequest
	lastSessionOpen      *apipb.ExecSessionOpen
	lastKillRequest      *apipb.SignalContainerRequest
	killError            error
	execResponse         *apipb.ExecContainerResponse
	execError            error
	execSession          contract.Session
	execSessionErr       error
	statFileResponse     *apipb.StatFileResponse
	listDirResponse      *apipb.ListDirResponse
	readFileRequests     []*apipb.ReadFileRequest
	writeFileRequests    []*apipb.WriteFileRequest
	mkdirRequests        []*apipb.MkdirRequest
	removeRequests       []*apipb.RemoveRequest
	existsRequests       []*apipb.ExistsRequest
	copyRequests         []*apipb.CopyRequest
	moveRequests         []*apipb.MoveRequest
	chmodRequests        []*apipb.ChmodRequest
	touchRequests        []*apipb.TouchRequest
	uploadRequests       []*apipb.UploadArchiveRequest
	downloadRequests     []*apipb.DownloadArchiveRequest
	fileOptions          []contract.HandlerOptions
	containerSpec        *specs.Spec
	containerSpecError   error
	createMetadataLabels map[string]string
}

func (h *runtimeSpyHandler) Name() string { return h.name }

func (h *runtimeSpyHandler) Capabilities() contract.RuntimeCapabilities { return h.capabilities }

func (h *runtimeSpyHandler) Requirements() contract.RuntimeRequirements { return h.requirements }

func (h *runtimeSpyHandler) Version(_ context.Context) (*apipb.RuntimeVersion, error) {
	return &apipb.RuntimeVersion{RuntimeName: h.name, RuntimeVersion: "test"}, nil
}

func (h *runtimeSpyHandler) CreateContainer(_ context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	h.createCalls++
	h.lastOptions = options
	h.lastRequest = request
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

func (h *runtimeSpyHandler) DeleteContainer(_ context.Context, _ *apipb.DeleteContainerRequest, _ contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	h.deleteCalls++
	return &apipb.DeleteContainerResponse{}, nil
}

func (h *runtimeSpyHandler) KillContainer(_ context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	h.killCalls++
	h.lastKillRequest = request
	h.lastOptions = options
	if h.killError != nil {
		return &apipb.SignalContainerResponse{}, h.killError
	}
	return &apipb.SignalContainerResponse{}, nil
}

func (h *runtimeSpyHandler) ListContainers(_ context.Context, _ contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return []*contract.UnionContainerState{}, nil
}

func (h *runtimeSpyHandler) ContainerSpec(_ context.Context, _ contract.HandlerOptions) (*specs.Spec, error) {
	if h.containerSpec != nil || h.containerSpecError != nil {
		return h.containerSpec, h.containerSpecError
	}
	return &specs.Spec{}, nil
}

func (h *runtimeSpyHandler) ExecContainer(_ context.Context, req *apipb.ExecContainerRequest, options contract.HandlerOptions) (*apipb.ExecContainerResponse, error) {
	h.lastExecRequest = req
	h.lastExecOptions = options
	if h.execResponse != nil || h.execError != nil {
		return h.execResponse, h.execError
	}
	return &apipb.ExecContainerResponse{}, nil
}

func (h *runtimeSpyHandler) OpenExecSession(_ context.Context, req *apipb.ExecSessionOpen, options contract.HandlerOptions) (contract.Session, error) {
	h.lastSessionOpen = req
	h.lastSessionOptions = options
	if h.execSession != nil || h.execSessionErr != nil {
		return h.execSession, h.execSessionErr
	}
	return &execSessionStub{}, nil
}

func (h *runtimeSpyHandler) ProcessService() contract.ProcessService {
	return runtimeSpyProcessService{handler: h}
}

func (h *runtimeSpyHandler) FileService() contract.FileService {
	return runtimeSpyFileService{handler: h}
}

func (h *runtimeSpyHandler) CheckpointContainer(*apipb.CheckpointRequest) error { return nil }

func (h *runtimeSpyHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	if h.waitFunc != nil {
		return h.waitFunc(ctx, options)
	}
	return contract.Exit{Status: h.waitExitCode}, nil
}

func (h *runtimeSpyHandler) ShutDown() {}
