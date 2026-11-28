package node

import (
	"context"

	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *Server) PreviewHTTP(ctx context.Context, req *nodesandboxv1.PreviewHTTPRequest) (*nodesandboxv1.PreviewHTTPResponse, error) {
	var response *nodesandboxv1.PreviewHTTPResponse
	err := s.unaryForPurpose(ctx, req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_HTTP_PREVIEW, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.PreviewHTTP(backendCtx, req)
		return err
	})
	return response, err
}
