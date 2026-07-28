package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appstorage "github.com/cofy-x/axern/control/storaged/internal/application/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	storagev1.UnimplementedStorageControlServer
	privatestoragev1.UnimplementedStorageCoordinatorServer

	controller *appstorage.Controller
}

func NewServer(controller *appstorage.Controller) *Server {
	return &Server{controller: controller}
}

func (s *Server) CreateVolumeClass(ctx context.Context, req *storagev1.CreateVolumeClassRequest) (*storagev1.CreateVolumeClassResponse, error) {
	out, err := s.controller.CreateVolumeClass(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.CreateVolumeClassResponse{VolumeClass: out}, nil
}

func (s *Server) GetVolumeClass(ctx context.Context, req *storagev1.GetVolumeClassRequest) (*storagev1.GetVolumeClassResponse, error) {
	out, ok, err := s.controller.GetVolumeClass(ctx, req.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	if !ok {
		return nil, status.Errorf(codes.NotFound, "volume class %q not found", req.GetName())
	}
	return &storagev1.GetVolumeClassResponse{VolumeClass: out}, nil
}

func (s *Server) ListVolumeClasses(ctx context.Context, _ *storagev1.ListVolumeClassesRequest) (*storagev1.ListVolumeClassesResponse, error) {
	out, err := s.controller.ListVolumeClasses(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.ListVolumeClassesResponse{VolumeClasses: out}, nil
}

func (s *Server) CreateVolumeClaim(ctx context.Context, req *storagev1.CreateVolumeClaimRequest) (*storagev1.CreateVolumeClaimResponse, error) {
	out, err := s.controller.CreateVolumeClaim(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.CreateVolumeClaimResponse{VolumeClaim: out}, nil
}

func (s *Server) GetVolumeClaim(ctx context.Context, req *storagev1.GetVolumeClaimRequest) (*storagev1.GetVolumeClaimResponse, error) {
	out, ok, err := s.controller.GetVolumeClaim(ctx, req.GetNamespace(), req.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	if !ok {
		return nil, status.Errorf(codes.NotFound, "volume claim %q/%q not found", req.GetNamespace(), req.GetName())
	}
	return &storagev1.GetVolumeClaimResponse{VolumeClaim: out}, nil
}

func (s *Server) ListVolumeClaims(ctx context.Context, req *storagev1.ListVolumeClaimsRequest) (*storagev1.ListVolumeClaimsResponse, error) {
	out, err := s.controller.ListVolumeClaims(ctx, req.GetFilter())
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.ListVolumeClaimsResponse{VolumeClaims: out}, nil
}

func (s *Server) UpdateVolumeClaim(ctx context.Context, req *storagev1.UpdateVolumeClaimRequest) (*storagev1.UpdateVolumeClaimResponse, error) {
	out, err := s.controller.UpdateVolumeClaim(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.UpdateVolumeClaimResponse{VolumeClaim: out}, nil
}

func (s *Server) DeleteVolumeClaim(ctx context.Context, req *storagev1.DeleteVolumeClaimRequest) (*storagev1.DeleteVolumeClaimResponse, error) {
	out, err := s.controller.DeleteVolumeClaim(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return &storagev1.DeleteVolumeClaimResponse{VolumeClaim: out}, nil
}

func (s *Server) DeleteWorkloadVolumeClaims(ctx context.Context, req *privatestoragev1.DeleteWorkloadVolumeClaimsRequest) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error) {
	return s.controller.DeleteWorkloadVolumeClaims(ctx, req)
}

func (s *Server) ReportVolumeReclaim(ctx context.Context, req *privatestoragev1.ReportVolumeReclaimRequest) (*privatestoragev1.ReportVolumeReclaimResponse, error) {
	return s.controller.ReportVolumeReclaim(ctx, req)
}

func (s *Server) ListVolumeReclaims(ctx context.Context, req *privatestoragev1.ListVolumeReclaimsRequest) (*privatestoragev1.ListVolumeReclaimsResponse, error) {
	return s.controller.ListVolumeReclaims(ctx, req)
}

func (s *Server) ClaimVolumeReclaims(ctx context.Context, req *privatestoragev1.ClaimVolumeReclaimsRequest) (*privatestoragev1.ClaimVolumeReclaimsResponse, error) {
	return s.controller.ClaimVolumeReclaims(ctx, req)
}

func (s *Server) GetVolumeReclaimQueueHealth(ctx context.Context, _ *privatestoragev1.VolumeReclaimQueueHealthRequest) (*privatestoragev1.VolumeReclaimQueueHealthResponse, error) {
	health, err := s.controller.GetVolumeReclaimQueueHealth(ctx)
	return &privatestoragev1.VolumeReclaimQueueHealthResponse{Health: health}, err
}

func (s *Server) ResolveVolumeRequirements(ctx context.Context, req *privatestoragev1.ResolveVolumeRequirementsRequest) (*privatestoragev1.ResolveVolumeRequirementsResponse, error) {
	out, err := s.controller.ResolveVolumeRequirements(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) ReserveVolumeBinding(ctx context.Context, req *privatestoragev1.ReserveVolumeBindingRequest) (*privatestoragev1.ReserveVolumeBindingResponse, error) {
	out, err := s.controller.ReserveVolumeBinding(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) ReleaseVolumeBinding(ctx context.Context, req *privatestoragev1.ReleaseVolumeBindingRequest) (*privatestoragev1.ReleaseVolumeBindingResponse, error) {
	out, err := s.controller.ReleaseVolumeBinding(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) RetryFailedVolumeBinding(ctx context.Context, req *privatestoragev1.RetryFailedVolumeBindingRequest) (*privatestoragev1.RetryFailedVolumeBindingResponse, error) {
	out, err := s.controller.RetryFailedVolumeBinding(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) ListVolumeBindings(ctx context.Context, req *privatestoragev1.ListVolumeBindingsRequest) (*privatestoragev1.ListVolumeBindingsResponse, error) {
	out, err := s.controller.ListVolumeBindings(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) ReportVolumePublish(ctx context.Context, req *privatestoragev1.ReportVolumePublishRequest) (*privatestoragev1.ReportVolumePublishResponse, error) {
	out, err := s.controller.ReportVolumePublish(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) ReportVolumeRelease(ctx context.Context, req *privatestoragev1.ReportVolumeReleaseRequest) (*privatestoragev1.ReportVolumeReleaseResponse, error) {
	out, err := s.controller.ReportVolumeRelease(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	return out, nil
}

func (s *Server) GetVolumeBindingHealth(ctx context.Context, req *privatestoragev1.VolumeBindingHealthRequest) (*privatestoragev1.VolumeBindingHealthResponse, error) {
	var releasingStuckAfter time.Duration
	if req != nil && req.GetReleasingStuckAfter() != nil {
		if err := req.GetReleasingStuckAfter().CheckValid(); err != nil {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("releasing_stuck_after is invalid: %v", err))
		}
		releasingStuckAfter = req.GetReleasingStuckAfter().AsDuration()
		if releasingStuckAfter < 0 {
			return nil, status.Error(codes.InvalidArgument, "releasing_stuck_after must be non-negative")
		}
	}
	out, err := s.controller.GetVolumeBindingHealth(ctx, releasingStuckAfter)
	if err != nil {
		return nil, rpcError(err)
	}
	return &privatestoragev1.VolumeBindingHealthResponse{Health: out}, nil
}

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	var st interface{ GRPCStatus() *status.Status }
	if errors.As(err, &st) {
		return err
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return status.Error(codes.NotFound, msg)
	case strings.Contains(msg, "already exists"), strings.Contains(msg, "duplicate key"):
		return status.Error(codes.AlreadyExists, msg)
	case strings.Contains(msg, "already bound"), strings.Contains(msg, "version mismatch"):
		return status.Error(codes.FailedPrecondition, msg)
	case strings.Contains(msg, "required"), strings.Contains(msg, "invalid"), strings.Contains(msg, "duplicated"), strings.Contains(msg, "unsupported"):
		return status.Error(codes.InvalidArgument, msg)
	default:
		return status.Error(codes.Internal, msg)
	}
}
