package nodebridge

import (
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type ProcessStreamer interface {
	Process(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandbox_ProcessClient, error)
}
