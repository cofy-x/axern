package api

import (
	"context"
	"strings"

	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationTunnelServer struct {
	nodetunnelv1.UnimplementedAllocationTunnelServer
	svc service.AllocationTunnelService
}

func NewAllocationTunnelServer(svc service.AllocationTunnelService) nodetunnelv1.AllocationTunnelServer {
	return &allocationTunnelServer{svc: svc}
}

func (s *allocationTunnelServer) ValidateAllocationTunnel(_ context.Context, req *nodetunnelv1.ValidateAllocationTunnelRequest) (*nodetunnelv1.ValidateAllocationTunnelResponse, error) {
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if err := s.svc.ValidateAllocationTunnel(allocationID); err != nil {
		return nil, errord.ToGRPC(err)
	}
	return &nodetunnelv1.ValidateAllocationTunnelResponse{
		AllocationID: allocationID,
	}, nil
}
