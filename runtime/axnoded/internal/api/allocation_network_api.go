package api

import (
	"context"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	nodenetworkv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/network/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationNetworkServer struct {
	nodenetworkv1.UnimplementedAllocationNetworkServer
	svc service.AllocationNetworkService
}

func NewAllocationNetworkServer(svc service.AllocationNetworkService) nodenetworkv1.AllocationNetworkServer {
	return &allocationNetworkServer{svc: svc}
}

func (s *allocationNetworkServer) ResolveAllocationNetwork(_ context.Context, req *nodenetworkv1.ResolveAllocationNetworkRequest) (*nodenetworkv1.ResolveAllocationNetworkResponse, error) {
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	network, err := s.svc.ResolveAllocationNetwork(allocationID)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if network == nil || strings.TrimSpace(network.NetNSPath) == "" {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "allocation %q has no usable network namespace", allocationID)
	}
	return &nodenetworkv1.ResolveAllocationNetworkResponse{
		AllocationID: allocationID,
		Ip:           network.IP,
		NetnsPath:    network.NetNSPath,
	}, nil
}
