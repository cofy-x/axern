package publicv1

import (
	"context"
	"errors"
	"strings"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateRun(ctx context.Context, req *runv1.CreateRunRequest) (*runv1.CreateRunResponse, error) {
	ctx, op := publicOps.Run(ctx, ctrlobs.SpanRunCreate, publicActionCreate, withEnvironmentID(req.GetEnvironmentID()))
	var opErr error
	defer func() { op.End(opErr) }()
	if err := validateExecutionConfigSecretRefs(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateOptionalExecutionArgv(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigResources(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateExecutionConfigImageMounts(req.GetConfig()); err != nil {
		opErr = err
		return nil, err
	}
	if err := validateNoServiceVolumeMounts(req.GetConfig(), "run"); err != nil {
		opErr = err
		return nil, err
	}
	run, err := s.deps.Runs.CreateRun(ctx, runkernel.CreateParams{
		Namespace:     req.GetNamespace(),
		EnvironmentID: req.GetEnvironmentID(),
		Config:        req.GetConfig(),
		Labels:        req.GetLabels(),
	}, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrRunID, run.GetID()))
	return &runv1.CreateRunResponse{Run: run}, nil
}

func (s *Server) GetRun(ctx context.Context, req *runv1.GetRunRequest) (*runv1.GetRunResponse, error) {
	id := strings.TrimSpace(req.GetRunID())
	if id == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "run_id is required")
	}
	run, err := s.deps.Runs.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return &runv1.GetRunResponse{Run: run}, nil
}

func (s *Server) WatchRun(req *runv1.WatchRunRequest, stream runv1.RunControl_WatchRunServer) error {
	id := strings.TrimSpace(req.GetRunID())
	if id == "" {
		return grpcstatus.Error(codes.InvalidArgument, "run_id is required")
	}
	if req.GetAfterVersion() < 0 {
		return grpcstatus.Error(codes.InvalidArgument, "after_version must be non-negative")
	}

	after := req.GetAfterVersion()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := s.deps.Runs.GetRun(stream.Context(), id)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if run.GetVersion() < after {
			return grpcstatus.Error(codes.InvalidArgument, "after_version is newer than the current run version")
		}
		if run.GetVersion() > after {
			if err := stream.Send(&runv1.WatchRunResponse{Run: run}); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			after = run.GetVersion()
			if isTerminalRun(run.GetStatus()) {
				return nil
			}
		} else if isTerminalRun(run.GetStatus()) {
			return nil
		}
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Server) ListRuns(ctx context.Context, req *runv1.ListRunsRequest) (*runv1.ListRunsResponse, error) {
	runs, err := s.deps.Runs.ListRuns(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &runv1.ListRunsResponse{Runs: runs}, nil
}

func (s *Server) CancelRun(ctx context.Context, req *runv1.CancelRunRequest) (*runv1.CancelRunResponse, error) {
	id := strings.TrimSpace(req.GetRunID())
	ctx, op := publicOps.Run(ctx, ctrlobs.SpanRunCancel, publicActionCancel, withRunID(id))
	var opErr error
	defer func() { op.End(opErr) }()
	if id == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "run_id is required")
		return nil, opErr
	}
	run, err := s.deps.Runs.CancelRun(ctx, id, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &runv1.CancelRunResponse{Run: run}, nil
}

func isTerminalRun(status runv1.RunStatus) bool {
	switch status {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED, runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}
