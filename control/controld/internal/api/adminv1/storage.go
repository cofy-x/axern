package adminv1

import (
	"context"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListStorageBindings(ctx context.Context, req *adminv1.ListStorageBindingsRequest) (*adminv1.ListStorageBindingsResponse, error) {
	if s.deps.Storage == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "storage admin is unavailable")
	}
	bindings, err := s.deps.Storage.ListStorageBindings(ctx, storageBindingFilterFromProto(req))
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.StorageBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, storageBindingToProto(binding))
	}
	return &adminv1.ListStorageBindingsResponse{Bindings: out}, nil
}

func (s *Server) RetryStorageBinding(ctx context.Context, req *adminv1.RetryStorageBindingRequest) (*adminv1.RetryStorageBindingResponse, error) {
	if s.deps.Storage == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "storage admin is unavailable")
	}
	binding, err := s.deps.Storage.RetryStorageBinding(ctx, adminkernel.RetryStorageBindingRequest{
		BindingID:      req.GetBindingID(),
		OperatorReason: req.GetOperatorReason(),
	})
	if err != nil {
		return nil, err
	}
	return &adminv1.RetryStorageBindingResponse{Binding: storageBindingToProto(*binding)}, nil
}

func (s *Server) ListStorageReclaims(ctx context.Context, req *adminv1.ListStorageReclaimsRequest) (*adminv1.ListStorageReclaimsResponse, error) {
	if s.deps.Storage == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "storage admin is unavailable")
	}
	reclaims, err := s.deps.Storage.ListStorageReclaims(ctx, adminkernel.StorageReclaimFilter{
		Namespace: req.GetNamespace(), ServiceID: req.GetServiceID(), NodeID: req.GetNodeID(), Limit: int(req.GetLimit()),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.StorageReclaim, 0, len(reclaims))
	for _, reclaim := range reclaims {
		item := &adminv1.StorageReclaim{
			ClaimID: reclaim.ClaimID, Namespace: reclaim.Namespace, ClaimName: reclaim.ClaimName,
			ServiceID: reclaim.ServiceID, NodeID: reclaim.NodeID, Attempt: reclaim.Attempt, LastError: reclaim.LastError,
		}
		if !reclaim.NextRetryAt.IsZero() {
			item.NextRetryAt = timestamppb.New(reclaim.NextRetryAt)
		}
		if !reclaim.UpdatedAt.IsZero() {
			item.UpdatedAt = timestamppb.New(reclaim.UpdatedAt)
		}
		out = append(out, item)
	}
	return &adminv1.ListStorageReclaimsResponse{Reclaims: out}, nil
}

func storageBindingFilterFromProto(req *adminv1.ListStorageBindingsRequest) adminkernel.StorageBindingFilter {
	if req == nil {
		return adminkernel.StorageBindingFilter{}
	}
	filter := req.GetFilter()
	return adminkernel.StorageBindingFilter{
		Statuses:     append([]storagev1.VolumeStatus(nil), filter.GetStatuses()...),
		Namespace:    filter.GetNamespace(),
		ClaimName:    filter.GetClaimName(),
		WorkloadID:   filter.GetWorkloadID(),
		AllocationID: filter.GetAllocationID(),
		NodeID:       filter.GetNodeID(),
		Limit:        int(req.GetLimit()),
	}
}

func storageBindingToProto(binding adminkernel.StorageBinding) *adminv1.StorageBinding {
	out := &adminv1.StorageBinding{
		BindingID:    binding.BindingID,
		ClaimID:      binding.ClaimID,
		Namespace:    binding.Namespace,
		ClaimName:    binding.ClaimName,
		WorkloadID:   binding.WorkloadID,
		WorkloadType: binding.WorkloadType,
		AllocationID: binding.AllocationID,
		NodeID:       binding.NodeID,
		Status:       binding.Status,
		Message:      binding.Message,
	}
	if !binding.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(binding.CreatedAt)
	}
	if !binding.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(binding.UpdatedAt)
	}
	if !binding.PublishedAt.IsZero() {
		out.PublishedAt = timestamppb.New(binding.PublishedAt)
	}
	if !binding.ReleasedAt.IsZero() {
		out.ReleasedAt = timestamppb.New(binding.ReleasedAt)
	}
	return out
}
