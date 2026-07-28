package publicv1

import (
	"context"
	"strings"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateEnvironment(ctx context.Context, req *environmentv1.CreateEnvironmentRequest) (*environmentv1.CreateEnvironmentResponse, error) {
	ctx, op := publicOps.Environment(ctx, ctrlobs.SpanEnvironmentCreate, publicActionCreate)
	var opErr error
	defer func() { op.End(opErr) }()
	spec := req.GetSpec()
	if spec == nil {
		opErr = grpcstatus.Error(codes.InvalidArgument, "spec is required")
		return nil, opErr
	}
	env, err := s.deps.Environments.CreateEnvironment(ctx, spec, req.GetLabels(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrEnvironmentID, env.GetID()))
	return &environmentv1.CreateEnvironmentResponse{Environment: env}, nil
}

func (s *Server) GetEnvironment(ctx context.Context, req *environmentv1.GetEnvironmentRequest) (*environmentv1.GetEnvironmentResponse, error) {
	id := strings.TrimSpace(req.GetEnvironmentID())
	if id == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "environment_id is required")
	}
	env, err := s.deps.Environments.GetEnvironment(ctx, id)
	if err != nil {
		return nil, err
	}
	return &environmentv1.GetEnvironmentResponse{Environment: env}, nil
}

func (s *Server) ListEnvironments(ctx context.Context, req *environmentv1.ListEnvironmentsRequest) (*environmentv1.ListEnvironmentsResponse, error) {
	envs, err := s.deps.Environments.ListEnvironments(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &environmentv1.ListEnvironmentsResponse{Environments: envs}, nil
}

func (s *Server) DeleteEnvironment(ctx context.Context, req *environmentv1.DeleteEnvironmentRequest) (*environmentv1.DeleteEnvironmentResponse, error) {
	id := strings.TrimSpace(req.GetEnvironmentID())
	ctx, op := publicOps.Environment(ctx, ctrlobs.SpanEnvironmentDelete, publicActionDelete, withEnvironmentID(id))
	var opErr error
	defer func() { op.End(opErr) }()
	if id == "" {
		opErr = grpcstatus.Error(codes.InvalidArgument, "environment_id is required")
		return nil, opErr
	}
	env, err := s.deps.Environments.DeleteEnvironment(ctx, id, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &environmentv1.DeleteEnvironmentResponse{Environment: env}, nil
}
