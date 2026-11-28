package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *nodeSandboxServer) StartBackgroundProcess(ctx context.Context, req *nodesandboxv1.StartBackgroundProcessRequest) (*nodesandboxv1.StartBackgroundProcessResponse, error) {
	processes, err := s.backgroundProcesses(ctx, req.GetAllocationID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	spec := req.GetSpec()
	timeout := spec.GetTimeoutSeconds()
	if len(spec.GetArgv()) == 0 || spec.GetTty() || timeout < 1 || timeout > wire.MaxBackgroundProcessSeconds || !wire.ValidBackgroundProcessRef(req.GetCallerRef()) {
		return nil, grpcstatus.Error(codes.InvalidArgument, "background process requires argv, a timeout from 1 to 3600 seconds, a valid caller reference, and no TTY")
	}
	status, err := processes.StartBackground(ctx, wire.BackgroundProcessSpec{
		Argv: spec.GetArgv(), Env: spec.GetEnv(), Cwd: spec.GetCwd(), User: spec.GetUser(),
		CallerRef: req.GetCallerRef(), Timeout: time.Duration(timeout) * time.Second,
	})
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	return &nodesandboxv1.StartBackgroundProcessResponse{Process: backgroundStatus(status)}, nil
}

func (s *nodeSandboxServer) GetBackgroundProcess(ctx context.Context, req *nodesandboxv1.GetBackgroundProcessRequest) (*nodesandboxv1.GetBackgroundProcessResponse, error) {
	processes, err := s.backgroundProcesses(ctx, req.GetAllocationID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if err := validateBackgroundProcessID(req.GetProcessID()); err != nil {
		return nil, errord.ToGRPC(err)
	}
	status, err := processes.GetBackground(ctx, req.GetProcessID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	return &nodesandboxv1.GetBackgroundProcessResponse{Process: backgroundStatus(status)}, nil
}

func (s *nodeSandboxServer) ListBackgroundProcesses(ctx context.Context, req *nodesandboxv1.ListBackgroundProcessesRequest) (*nodesandboxv1.ListBackgroundProcessesResponse, error) {
	processes, err := s.backgroundProcesses(ctx, req.GetAllocationID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	values, err := processes.ListBackground(ctx)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	statuses := make([]*nodesandboxv1.BackgroundProcess, 0, len(values))
	for _, status := range values {
		statuses = append(statuses, backgroundStatus(status))
	}
	return &nodesandboxv1.ListBackgroundProcessesResponse{Processes: statuses}, nil
}

func (s *nodeSandboxServer) CancelBackgroundProcess(ctx context.Context, req *nodesandboxv1.CancelBackgroundProcessRequest) (*nodesandboxv1.CancelBackgroundProcessResponse, error) {
	processes, err := s.backgroundProcesses(ctx, req.GetAllocationID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if err := validateBackgroundProcessID(req.GetProcessID()); err != nil {
		return nil, errord.ToGRPC(err)
	}
	status, err := processes.CancelBackground(ctx, req.GetProcessID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	return &nodesandboxv1.CancelBackgroundProcessResponse{Process: backgroundStatus(status)}, nil
}

func (s *nodeSandboxServer) backgroundProcesses(ctx context.Context, allocationID string) (contract.BackgroundProcesses, error) {
	target, err := s.validateDirectAuth(ctx, allocationID)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if s.localOnly {
		return nil, grpcstatus.Error(codes.PermissionDenied, "background processes require the authenticated gateway")
	}
	background, ok := s.svc.(service.SandboxBackgroundService)
	if !ok {
		return nil, grpcstatus.Error(codes.Unavailable, "background processes are unavailable")
	}
	processes, err := background.BackgroundProcesses(target.targetID)
	return processes, errord.ToGRPC(err)
}

func validateBackgroundProcessID(id string) error {
	sequence, ok := strings.CutPrefix(id, "proc-")
	number, err := strconv.ParseUint(sequence, 10, 64)
	if !ok || err != nil || number == 0 || sequence != strconv.FormatUint(number, 10) {
		return grpcstatus.Error(codes.InvalidArgument, "invalid background process ID")
	}
	return nil
}

func backgroundStatus(status wire.ProcessStatus) *nodesandboxv1.BackgroundProcess {
	value := &nodesandboxv1.BackgroundProcess{ProcessID: status.ID, CallerRef: status.CallerRef}
	switch status.State {
	case "starting":
		value.State = nodesandboxv1.BackgroundProcessState_BACKGROUND_PROCESS_STATE_STARTING
	case "running":
		value.State = nodesandboxv1.BackgroundProcessState_BACKGROUND_PROCESS_STATE_RUNNING
	case "exited":
		value.State = nodesandboxv1.BackgroundProcessState_BACKGROUND_PROCESS_STATE_EXITED
	case "failed":
		value.State = nodesandboxv1.BackgroundProcessState_BACKGROUND_PROCESS_STATE_FAILED
	}
	if status.ExitCode != nil {
		value.ExitCode = proto.Int32(int32(*status.ExitCode))
	}
	if status.StartedAt != nil {
		value.StartedAt = timestamppb.New(*status.StartedAt)
	}
	if status.FinishedAt != nil {
		value.FinishedAt = timestamppb.New(*status.FinishedAt)
	}
	return value
}
