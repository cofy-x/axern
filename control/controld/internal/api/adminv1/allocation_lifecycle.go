package adminv1

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	privateadminv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/admin/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListAllocationLifecycleRetries(ctx context.Context, req *privateadminv1.ListAllocationLifecycleRetriesRequest) (*privateadminv1.ListAllocationLifecycleRetriesResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	items, err := s.deps.AllocationLifecycleRetries.ListAllocationLifecycleRetries(ctx, lifecycleRetryFilterFromProto(req), now)
	if err != nil {
		return nil, err
	}
	out := make([]*privateadminv1.AllocationLifecycleRetry, 0, len(items))
	for _, item := range items {
		out = append(out, lifecycleRetryToProto(item))
	}
	return &privateadminv1.ListAllocationLifecycleRetriesResponse{Retries: out}, nil
}

func (s *Server) ForceAllocationLifecycleRetry(ctx context.Context, req *privateadminv1.ForceAllocationLifecycleRetryRequest) (*privateadminv1.ForceAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.ForceAllocationLifecycleRetry(ctx, allocationkernel.ForceLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		OperatorReason: req.GetOperatorReason(),
		RequestedRunAt: now,
	}, now)
	if err != nil {
		return nil, err
	}
	return &privateadminv1.ForceAllocationLifecycleRetryResponse{Retry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) FailAllocationLifecycleRetry(ctx context.Context, req *privateadminv1.FailAllocationLifecycleRetryRequest) (*privateadminv1.FailAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.FailAllocationLifecycleRetry(ctx, allocationkernel.FailLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		OperatorReason: req.GetOperatorReason(),
	}, now)
	if err != nil {
		return nil, err
	}
	return &privateadminv1.FailAllocationLifecycleRetryResponse{FailedRetry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) ClearAllocationLifecycleRetry(ctx context.Context, req *privateadminv1.ClearAllocationLifecycleRetryRequest) (*privateadminv1.ClearAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.ClearAllocationLifecycleRetry(ctx, allocationkernel.ClearLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		OperatorReason: req.GetOperatorReason(),
	}, now)
	if err != nil {
		return nil, err
	}
	return &privateadminv1.ClearAllocationLifecycleRetryResponse{ClearedRetry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now().UTC()
}

func lifecycleRetryFilterFromProto(req *privateadminv1.ListAllocationLifecycleRetriesRequest) allocationkernel.LifecycleRetryFilter {
	if req == nil {
		return allocationkernel.LifecycleRetryFilter{}
	}
	filter := req.GetFilter()
	return allocationkernel.LifecycleRetryFilter{
		DueOnly: filter.GetDueOnly(),
		Limit:   int(req.GetLimit()),
	}
}

func lifecycleRetryToProto(item allocationkernel.LifecycleRetryItem) *privateadminv1.AllocationLifecycleRetry {
	return &privateadminv1.AllocationLifecycleRetry{
		AllocationID:       item.AllocationID,
		RunID:              item.RunID,
		EnvironmentID:      item.EnvironmentID,
		LifecycleState:     allocationkernel.ParseLifecycleState(item.LifecycleState),
		NodeID:             item.NodeID,
		NodeTarget:         item.NodeTarget,
		ReconcileAttempts:  int32(item.ReconcileAttempts),
		LastError:          item.LastReconcileError,
		NextRunAt:          timestamppb.New(item.NextRunAt),
		CreatedAt:          timestamppb.New(item.CreatedAt),
		UpdatedAt:          timestamppb.New(item.UpdatedAt),
		AgeSeconds:         item.AgeSeconds,
		Due:                item.Due,
		Clearable:          item.Clearable,
		ClearBlockedReason: item.ClearBlockedReason,
	}
}
