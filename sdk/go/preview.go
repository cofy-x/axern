package axernsdk

import (
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

// HTTPPreview contains one bounded response from an Allocation's loopback service.
type HTTPPreview struct {
	Status      int
	ContentType string
	Body        []byte
	Location    string
}

// PreviewHTTP sends a GET or HEAD to a guest port using the client's
// authenticated gateway session. Target must be a local request path.
func (n *AllocationClient) PreviewHTTP(ctx context.Context, port uint32, method, target string) (HTTPPreview, error) {
	if err := n.validate(); err != nil {
		return HTTPPreview{}, err
	}
	response, err := n.client.nodes.PreviewHTTP(ctx, &nodesandboxv1.PreviewHTTPRequest{
		AllocationID: n.allocationID, Port: port, Method: method, Target: target,
	})
	if err != nil {
		return HTTPPreview{}, mapRPCError(err, "preview HTTP", n.allocationID)
	}
	return HTTPPreview{
		Status: int(response.GetStatus()), ContentType: response.GetContentType(),
		Body: response.GetBody(), Location: response.GetLocation(),
	}, nil
}
