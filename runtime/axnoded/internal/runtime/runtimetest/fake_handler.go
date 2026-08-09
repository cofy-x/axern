package runtimetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func NewFakeRuntimeHandler() *FakeRuntimeHandler {
	return &FakeRuntimeHandler{
		RuntimeName: config.RuntimeNameRunsc,
		RuntimeCapabilities: contract.RuntimeCapabilities{
			CanCheckpoint: true,
			CanExecDirect: true,
		},
		RuntimeRequirements: contract.RuntimeRequirements{},
	}
}

type FakeRuntimeHandler struct {
	RuntimeName         string
	RuntimeCapabilities contract.RuntimeCapabilities
	RuntimeRequirements contract.RuntimeRequirements
}

func (f *FakeRuntimeHandler) Name() string {
	if f.RuntimeName == "" {
		return config.RuntimeNameRunsc
	}
	return f.RuntimeName
}

func (f *FakeRuntimeHandler) Capabilities() contract.RuntimeCapabilities {
	return f.RuntimeCapabilities
}

func (f *FakeRuntimeHandler) Requirements() contract.RuntimeRequirements {
	return f.RuntimeRequirements
}

func (f *FakeRuntimeHandler) Version(ctx context.Context) (*runtimeapi.RuntimeVersion, error) {
	return &runtimeapi.RuntimeVersion{
		RuntimeName:    f.Name(),
		RuntimeVersion: config.UnknownVersion,
	}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) CreateContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	return &apipb.ContainerMetadata{
		ID:             options.ContainerID,
		RuntimeHandler: f.Name(),
		Labels:         options.AdditionalAnnotations,
		Stdout:         request.GetStdout(),
		Stderr:         request.GetStderr(),
	}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) AllocationEnforcementManifest(_ context.Context, containerID string) (*apipb.AllocationEnforcementManifest, error) {
	return &apipb.AllocationEnforcementManifest{
		RuntimeName:       f.Name(),
		BundlePath:        "/fake/" + containerID,
		CreatedAtUnixNano: time.Now().UTC().UnixNano(),
	}, nil
}

func (f *FakeRuntimeHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	return &apipb.DeleteContainerResponse{}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) KillContainer(ctx context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	return &apipb.SignalContainerResponse{}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) ListContainers(ctx context.Context, options contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return []*contract.UnionContainerState{}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) ContainerSpec(ctx context.Context, options contract.HandlerOptions) (*spec.Spec, error) {
	return &spec.Spec{}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) ExecContainer(ctx context.Context, request *apipb.ExecContainerRequest, options contract.HandlerOptions) (*apipb.ExecContainerResponse, error) {
	return &apipb.ExecContainerResponse{}, getErrorFromContext(ctx)
}

type fakeExecSession struct{}

func (f *fakeExecSession) Write([]byte) error             { return nil }
func (f *fakeExecSession) CloseStdin() error              { return nil }
func (f *fakeExecSession) Resize(cols, rows uint32) error { return nil }
func (f *fakeExecSession) Signal(string) error            { return nil }
func (f *fakeExecSession) Recv() (contract.Chunk, error) {
	return contract.Chunk{}, io.EOF
}
func (f *fakeExecSession) Wait() (contract.Exit, error) {
	return contract.Exit{Timestamp: time.Time{}, Status: 0}, nil
}
func (f *fakeExecSession) Close() error { return nil }

func (f *FakeRuntimeHandler) OpenExecSession(ctx context.Context, request *apipb.ExecSessionOpen, options contract.HandlerOptions) (contract.Session, error) {
	if err := getErrorFromContext(ctx); err != nil {
		return nil, err
	}
	return &fakeExecSession{}, nil
}

func (f *FakeRuntimeHandler) ProcessService() contract.ProcessService {
	return fakeProcessService{}
}

type fakeProcessService struct{}

func (fakeProcessService) OpenProcess(ctx context.Context, request *apipb.ProcessOpen, options contract.HandlerOptions) (contract.Session, error) {
	if err := getErrorFromContext(ctx); err != nil {
		return nil, err
	}
	return &fakeExecSession{}, nil
}

func (f *FakeRuntimeHandler) FileService() contract.FileService {
	return fakeFileService{}
}

type fakeFileService struct{}

func (fakeFileService) StatFile(ctx context.Context, request *apipb.StatFileRequest, options contract.HandlerOptions) (*apipb.StatFileResponse, error) {
	return &apipb.StatFileResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) ListDir(ctx context.Context, request *apipb.ListDirRequest, options contract.HandlerOptions) (*apipb.ListDirResponse, error) {
	return &apipb.ListDirResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) ReadFile(ctx context.Context, request *apipb.ReadFileRequest, options contract.HandlerOptions) (*apipb.ReadFileResponse, error) {
	return &apipb.ReadFileResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) WriteFile(ctx context.Context, request *apipb.WriteFileRequest, options contract.HandlerOptions) (*apipb.WriteFileResponse, error) {
	return &apipb.WriteFileResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Mkdir(ctx context.Context, request *apipb.MkdirRequest, options contract.HandlerOptions) (*apipb.MkdirResponse, error) {
	return &apipb.MkdirResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Remove(ctx context.Context, request *apipb.RemoveRequest, options contract.HandlerOptions) (*apipb.RemoveResponse, error) {
	return &apipb.RemoveResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Exists(ctx context.Context, request *apipb.ExistsRequest, options contract.HandlerOptions) (*apipb.ExistsResponse, error) {
	return &apipb.ExistsResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Copy(ctx context.Context, request *apipb.CopyRequest, options contract.HandlerOptions) (*apipb.CopyResponse, error) {
	return &apipb.CopyResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Move(ctx context.Context, request *apipb.MoveRequest, options contract.HandlerOptions) (*apipb.MoveResponse, error) {
	return &apipb.MoveResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Chmod(ctx context.Context, request *apipb.ChmodRequest, options contract.HandlerOptions) (*apipb.ChmodResponse, error) {
	return &apipb.ChmodResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) Touch(ctx context.Context, request *apipb.TouchRequest, options contract.HandlerOptions) (*apipb.TouchResponse, error) {
	return &apipb.TouchResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) UploadArchive(ctx context.Context, request *apipb.UploadArchiveRequest, input io.Reader, options contract.HandlerOptions) (*apipb.UploadArchiveResponse, error) {
	_, _ = io.Copy(io.Discard, input)
	return &apipb.UploadArchiveResponse{}, getErrorFromContext(ctx)
}

func (fakeFileService) DownloadArchive(ctx context.Context, request *apipb.DownloadArchiveRequest, output io.Writer, options contract.HandlerOptions) (*apipb.DownloadArchiveResponse, error) {
	_, _ = output.Write([]byte("archive"))
	return &apipb.DownloadArchiveResponse{}, getErrorFromContext(ctx)
}

func (f *FakeRuntimeHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	return contract.Exit{
		Timestamp: time.Time{},
		Status:    0,
	}, getErrorFromContext(ctx)
}

func (r *FakeRuntimeHandler) ShutDown() {}

func (r *FakeRuntimeHandler) CheckpointContainer(*runtimeapi.CheckpointRequest) error {
	return fmt.Errorf("Not implemented")
}

func getErrorFromContext(ctx context.Context) error {
	if errStr, ok := ctx.Value("ERROR").(string); ok {
		return errors.New(errStr)
	}
	return nil
}

var _ contract.RuntimeHandler = &FakeRuntimeHandler{}
