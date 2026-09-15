package placementkernel

import "testing"

func TestEvaluationLessPrefersLowerRuntimeSlotOccupancy(t *testing.T) {
	candidate := func(nodeID string, active int64) *Evaluation {
		return &Evaluation{
			NodeID: nodeID,
			Rank:   &Rank{IdlePoolReady: true, RuntimeSlotOccupancy: active},
		}
	}

	if !EvaluationLess(candidate("node-b", 2), candidate("node-a", 3)) {
		t.Fatal("candidate with lower runtime slot occupancy should rank first")
	}
}
