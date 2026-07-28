package adminv1

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListAllocationLifecycleRetries(ctx context.Context, req *adminv1.ListAllocationLifecycleRetriesRequest) (*adminv1.ListAllocationLifecycleRetriesResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	items, err := s.deps.AllocationLifecycleRetries.ListAllocationLifecycleRetries(ctx, lifecycleRetryFilterFromProto(req), now)
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.AllocationLifecycleRetry, 0, len(items))
	for _, item := range items {
		out = append(out, lifecycleRetryToProto(item))
	}
	return &adminv1.ListAllocationLifecycleRetriesResponse{Retries: out}, nil
}

func (s *Server) ForceAllocationLifecycleRetry(ctx context.Context, req *adminv1.ForceAllocationLifecycleRetryRequest) (*adminv1.ForceAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.ForceAllocationLifecycleRetry(ctx, allocationkernel.ForceLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		Reason:         retryReasonFromProto(req.GetReason()),
		OperatorReason: req.GetOperatorReason(),
		RequestedRunAt: now,
	}, now)
	if err != nil {
		return nil, err
	}
	return &adminv1.ForceAllocationLifecycleRetryResponse{Retry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) FailAllocationLifecycleRetry(ctx context.Context, req *adminv1.FailAllocationLifecycleRetryRequest) (*adminv1.FailAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.FailAllocationLifecycleRetry(ctx, allocationkernel.FailLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		Reason:         retryReasonFromProto(req.GetReason()),
		OperatorReason: req.GetOperatorReason(),
	}, now)
	if err != nil {
		return nil, err
	}
	return &adminv1.FailAllocationLifecycleRetryResponse{FailedRetry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) ClearAllocationLifecycleRetry(ctx context.Context, req *adminv1.ClearAllocationLifecycleRetryRequest) (*adminv1.ClearAllocationLifecycleRetryResponse, error) {
	if s.deps.AllocationLifecycleRetries == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "allocation lifecycle admin is unavailable")
	}
	now := s.now()
	item, err := s.deps.AllocationLifecycleRetries.ClearAllocationLifecycleRetry(ctx, allocationkernel.ClearLifecycleRetryRequest{
		AllocationID:   req.GetAllocationID(),
		Reason:         retryReasonFromProto(req.GetReason()),
		OperatorReason: req.GetOperatorReason(),
	}, now)
	if err != nil {
		return nil, err
	}
	return &adminv1.ClearAllocationLifecycleRetryResponse{ClearedRetry: lifecycleRetryToProto(*item)}, nil
}

func (s *Server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now().UTC()
}

func lifecycleRetryFilterFromProto(req *adminv1.ListAllocationLifecycleRetriesRequest) allocationkernel.LifecycleRetryFilter {
	if req == nil {
		return allocationkernel.LifecycleRetryFilter{}
	}
	filter := req.GetFilter()
	return allocationkernel.LifecycleRetryFilter{
		OwnerType: ownerTypeFromProto(filter.GetOwnerType()),
		Reason:    retryReasonFromProto(filter.GetReason()),
		DueOnly:   filter.GetDueOnly(),
		Limit:     int(req.GetLimit()),
	}
}

func lifecycleRetryToProto(item allocationkernel.LifecycleRetryItem) *adminv1.AllocationLifecycleRetry {
	return &adminv1.AllocationLifecycleRetry{
		AllocationID:       item.AllocationID,
		OwnerID:            item.OwnerID,
		OwnerType:          ownerTypeToProto(item.OwnerType),
		EnvironmentID:      item.EnvironmentID,
		Reason:             retryReasonToProto(item.Reason),
		NodeID:             item.NodeID,
		NodeTarget:         item.NodeTarget,
		Attempt:            item.Attempt,
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

func ownerTypeFromProto(ownerType adminv1.AllocationLifecycleRetryOwnerType) string {
	switch ownerType {
	case adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN:
		return allocationkernel.OwnerRun
	case adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE:
		return allocationkernel.OwnerService
	default:
		return ""
	}
}

func ownerTypeToProto(ownerType string) adminv1.AllocationLifecycleRetryOwnerType {
	switch ownerType {
	case allocationkernel.OwnerRun:
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN
	case allocationkernel.OwnerService:
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE
	default:
		return adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_UNSPECIFIED
	}
}

func retryReasonFromProto(reason adminv1.AllocationLifecycleRetryReason) string {
	switch reason {
	case adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE:
		return allocationkernel.ReconcileReasonCreate
	case adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE:
		return allocationkernel.ReconcileReasonDelete
	default:
		return ""
	}
}

func retryReasonToProto(reason string) adminv1.AllocationLifecycleRetryReason {
	switch reason {
	case allocationkernel.ReconcileReasonCreate:
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE
	case allocationkernel.ReconcileReasonDelete:
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE
	default:
		return adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_UNSPECIFIED
	}
}
