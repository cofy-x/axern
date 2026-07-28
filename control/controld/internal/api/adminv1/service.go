package adminv1

import (
	"context"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) PurgeService(ctx context.Context, req *adminv1.PurgeServiceRequest) (*adminv1.PurgeServiceResponse, error) {
	if s.deps.Services == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "service admin is unavailable")
	}
	serviceID, err := s.deps.Services.PurgeService(ctx, strings.TrimSpace(req.GetServiceID()), strings.TrimSpace(req.GetOperatorReason()), s.now())
	if err != nil {
		return nil, err
	}
	return &adminv1.PurgeServiceResponse{ServiceID: serviceID}, nil
}
