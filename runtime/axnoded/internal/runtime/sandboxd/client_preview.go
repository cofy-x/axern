package sandboxd

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) PreviewHTTP(ctx context.Context, request wire.HTTPPreviewRequest) (wire.HTTPPreviewResponse, error) {
	var response wire.HTTPPreviewResponse
	err := c.postJSON(ctx, wire.PathHTTPPreview, request, &response)
	return response, OperationError("HTTP preview", "read", err)
}
