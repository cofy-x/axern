package api

import (
	"context"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) PreviewHTTP(ctx context.Context, req *nodesandboxv1.PreviewHTTPRequest) (*nodesandboxv1.PreviewHTTPResponse, error) {
	target, err := s.validateAccessPurpose(ctx, req.GetAllocationID(), gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_HTTP_PREVIEW)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if s.localOnly {
		return nil, grpcstatus.Error(codes.PermissionDenied, "HTTP preview requires the authenticated gateway")
	}
	preview, ok := s.svc.(service.SandboxHTTPPreviewService)
	if !ok {
		return nil, grpcstatus.Error(codes.Unavailable, "HTTP preview is unavailable")
	}
	input := wire.HTTPPreviewRequest{Port: req.GetPort(), Method: req.GetMethod(), Target: req.GetTarget()}
	if _, err := input.Destination(); err != nil {
		return nil, grpcstatus.Error(codes.InvalidArgument, err.Error())
	}
	result, err := preview.PreviewHTTP(ctx, target.targetID, input)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	return &nodesandboxv1.PreviewHTTPResponse{
		Status: int32(result.Status), ContentType: result.ContentType, Body: result.Body, Location: result.Location,
	}, nil
}
