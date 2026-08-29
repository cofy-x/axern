package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/runtime/egressd/internal/policy"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	runtimeegressv1.UnimplementedRuntimeEgressServiceServer
	manager *policy.Manager
}

func NewServer(manager *policy.Manager) *Server {
	return &Server{manager: manager}
}

func (s *Server) PreparePolicy(ctx context.Context, req *runtimeegressv1.PreparePolicyRequest) (*runtimeegressv1.PreparePolicyResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	record, alreadyPrepared, err := s.manager.Prepare(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetSandboxIp(), req.GetPolicy(), req.GetExecutionRevision())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimeegressv1.PreparePolicyResponse{Policy: record, AlreadyPrepared: alreadyPrepared}, nil
}

func (s *Server) DeletePolicy(ctx context.Context, req *runtimeegressv1.DeletePolicyRequest) (*runtimeegressv1.DeletePolicyResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	deleted, err := s.manager.Delete(ctx, req.GetAllocationID(), req.GetAttempt())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimeegressv1.DeletePolicyResponse{Deleted: deleted}, nil
}

func (s *Server) GetPolicy(_ context.Context, req *runtimeegressv1.GetPolicyRequest) (*runtimeegressv1.GetPolicyResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	record, ok := s.manager.Get(req.GetAllocationID(), req.GetAttempt())
	if !ok {
		return nil, status.Error(codes.NotFound, "prepared egress policy not found")
	}
	return &runtimeegressv1.GetPolicyResponse{Policy: record}, nil
}

func (s *Server) ListPolicies(_ context.Context, req *runtimeegressv1.ListPoliciesRequest) (*runtimeegressv1.ListPoliciesResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	return &runtimeegressv1.ListPoliciesResponse{Policies: s.manager.List(req.GetAllocationID())}, nil
}

func (s *Server) ReconcilePolicies(ctx context.Context, req *runtimeegressv1.ReconcilePoliciesRequest) (*runtimeegressv1.ReconcilePoliciesResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	result, err := s.manager.Reconcile(ctx, req.GetActivePolicies())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimeegressv1.ReconcilePoliciesResponse{
		ActivePolicyCount:        int32(result.ActivePolicyCount),
		RetainedCount:            int32(result.RetainedCount),
		DeletedCount:             int32(result.DeletedCount),
		StalePolicyCount:         int32(result.StalePolicyCount),
		InvalidActivePolicyCount: int32(result.InvalidActivePolicyCount),
	}, nil
}

func (s *Server) GetEgressManagerHealth(_ context.Context, _ *runtimeegressv1.EgressManagerHealthRequest) (*runtimeegressv1.EgressManagerHealthResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "egress policy manager is not configured")
	}
	return &runtimeegressv1.EgressManagerHealthResponse{Health: s.manager.Health()}, nil
}

func toStatus(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "required"), strings.Contains(msg, "must be"), strings.Contains(msg, "invalid"):
		return status.Error(codes.InvalidArgument, msg)
	case strings.Contains(msg, "stale"), strings.Contains(msg, "already prepared"), strings.Contains(msg, "already owned"):
		return status.Error(codes.FailedPrecondition, msg)
	default:
		return status.Error(codes.Internal, fmt.Errorf("egressd: %w", err).Error())
	}
}
