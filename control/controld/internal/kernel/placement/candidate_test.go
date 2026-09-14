package placementkernel

import "testing"

func TestEvaluationLessPrefersFewerActiveInstances(t *testing.T) {
	candidate := func(nodeID string, active int64) *Evaluation {
		return &Evaluation{
			NodeID: nodeID,
			Rank:   &Rank{IdlePoolReady: true, AxnodedActiveInstances: active},
		}
	}

	if !EvaluationLess(candidate("node-b", 2), candidate("node-a", 3)) {
		t.Fatal("candidate with fewer active instances should rank first")
	}
}
