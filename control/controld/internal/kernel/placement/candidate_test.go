package placementkernel

import (
	"testing"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestEvaluationLessPrefersFewerActiveInstances(t *testing.T) {
	candidate := func(nodeID string, active int64) *nodev1.PlacementCandidate {
		return &nodev1.PlacementCandidate{
			NodeID: nodeID,
			Rank:   &nodev1.PlacementRank{IdlePoolReady: true, AxnodedActiveInstances: active},
		}
	}

	if !EvaluationLess(candidate("node-b", 2), candidate("node-a", 3)) {
		t.Fatal("candidate with fewer active instances should rank first")
	}
}
