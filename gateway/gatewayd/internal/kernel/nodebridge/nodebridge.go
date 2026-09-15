package nodebridge

import (
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type ExecStreamer interface {
	ExecStream(ctx context.Context, target string) (nodesandboxv1.NodeSandbox_ExecStreamClient, error)
}
