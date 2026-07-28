package axernsdk

import (
	"context"
	"fmt"
	"strings"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type TaskAssetKind string

const (
	TaskAssetKindVerifier TaskAssetKind = "verifier"
	TaskAssetKindOracle   TaskAssetKind = "oracle"
)

// MaterializeTaskAssets makes protected assets from the allocation's resolved
// TaskSet payload visible inside its copy-on-write workspace.
func (s *Sandbox) MaterializeTaskAssets(ctx context.Context, sourcePath, target string, kind TaskAssetKind) error {
	node, err := s.nodeClient()
	if err != nil {
		return err
	}
	durationMs, err := node.MaterializeTaskAssets(ctx, sourcePath, target, kind)
	if err == nil && kind == TaskAssetKindVerifier {
		s.state.VerifierMaterializeMs += durationMs
	}
	return err
}

func (n *NodeSandboxClient) MaterializeTaskAssets(ctx context.Context, sourcePath, target string, kind TaskAssetKind) (int64, error) {
	if err := n.validate(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(target) == "" {
		return 0, fmt.Errorf("source_path and target are required")
	}
	var protoKind nodesandboxv1.TaskAssetKind
	switch kind {
	case TaskAssetKindVerifier:
		protoKind = nodesandboxv1.TaskAssetKind_TASK_ASSET_KIND_VERIFIER
	case TaskAssetKindOracle:
		protoKind = nodesandboxv1.TaskAssetKind_TASK_ASSET_KIND_ORACLE
	default:
		return 0, fmt.Errorf("task asset kind must be verifier or oracle")
	}
	durationMs, err := n.rpcClient().MaterializeTaskAssets(ctx, sourcePath, target, protoKind)
	return durationMs, mapRPCError(err, "materialize task assets", n.allocationID)
}
