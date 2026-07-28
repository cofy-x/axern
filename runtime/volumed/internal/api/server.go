package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/runtime/volumed/internal/storage"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	runtimevolumev1.UnimplementedRuntimeVolumeServiceServer
	manager *storage.Manager
}

func NewServer(manager *storage.Manager) *Server {
	return &Server{manager: manager}
}

func (s *Server) PublishVolume(ctx context.Context, req *runtimevolumev1.PublishVolumeRequest) (*runtimevolumev1.PublishVolumeResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, status.Error(codes.InvalidArgument, "allocation_id is required")
	}
	volume, err := s.manager.Publish(ctx, allocationID, req.GetRuntimeClass(), req.GetVolume())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimevolumev1.PublishVolumeResponse{Volume: volume}, nil
}

func (s *Server) UnpublishVolume(ctx context.Context, req *runtimevolumev1.UnpublishVolumeRequest) (*runtimevolumev1.UnpublishVolumeResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	volumes, err := s.manager.Unpublish(ctx, req.GetAllocationID(), req.GetBindingID())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimevolumev1.UnpublishVolumeResponse{Volumes: volumes}, nil
}

func (s *Server) DeleteVolume(ctx context.Context, req *runtimevolumev1.DeleteVolumeRequest) (*runtimevolumev1.DeleteVolumeResponse, error) {
	if err := s.manager.Delete(ctx, req.GetClaimID(), req.GetBackend(), req.GetBackendHandle()); err != nil {
		return nil, err
	}
	return &runtimevolumev1.DeleteVolumeResponse{}, nil
}

func (s *Server) GetPublishedVolume(ctx context.Context, req *runtimevolumev1.GetPublishedVolumeRequest) (*runtimevolumev1.GetPublishedVolumeResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	volume, ok := s.manager.Get(req.GetAllocationID(), req.GetBindingID())
	if !ok {
		return nil, status.Error(codes.NotFound, "published volume not found")
	}
	return &runtimevolumev1.GetPublishedVolumeResponse{Volume: volume}, nil
}

func (s *Server) ListPublishedVolumes(ctx context.Context, req *runtimevolumev1.ListPublishedVolumesRequest) (*runtimevolumev1.ListPublishedVolumesResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	return &runtimevolumev1.ListPublishedVolumesResponse{
		Volumes: s.manager.List(req.GetAllocationID()),
	}, nil
}

func (s *Server) ReconcileVolumes(ctx context.Context, req *runtimevolumev1.ReconcileVolumesRequest) (*runtimevolumev1.ReconcileVolumesResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	result, err := s.manager.Reconcile(ctx, req.GetActiveAllocationIds())
	if err != nil {
		return nil, toStatus(err)
	}
	return &runtimevolumev1.ReconcileVolumesResponse{
		ActiveAllocationCount: int32(result.ActiveAllocationCount),
		RetainedCount:         int32(result.RetainedCount),
		UnpublishedCount:      int32(result.UnpublishedCount),
		StaleAllocationCount:  int32(result.StaleAllocationCount),
		InvalidVolumeCount:    int32(result.InvalidVolumeCount),
	}, nil
}

func (s *Server) GetVolumeManagerHealth(ctx context.Context, _ *runtimevolumev1.VolumeManagerHealthRequest) (*runtimevolumev1.VolumeManagerHealthResponse, error) {
	if s == nil || s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "volume manager is not configured")
	}
	return &runtimevolumev1.VolumeManagerHealthResponse{Health: s.manager.Health()}, nil
}

func toStatus(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "required"), strings.Contains(msg, "invalid"), strings.Contains(msg, "not supported"), strings.Contains(msg, "does not support"):
		return status.Error(codes.InvalidArgument, msg)
	case strings.Contains(msg, "not found"):
		return status.Error(codes.NotFound, msg)
	default:
		return status.Error(codes.Internal, fmt.Errorf("volumed: %w", err).Error())
	}
}
