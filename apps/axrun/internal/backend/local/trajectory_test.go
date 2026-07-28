package local

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRunnerExecuteContinuesExistingTrajectoryIndexes(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	if err := store.AppendTrajectoryStep(layout.TrajectoryPath, domain.TrajectoryStep{
		Index:     1,
		Timestamp: fixedNow(),
		Type:      "system",
		Actor:     "test",
		Summary:   "preexisting step",
	}); err != nil {
		t.Fatalf("AppendTrajectoryStep returned error: %v", err)
	}
	_, err := (Adapter{Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if len(steps) != 6 {
		t.Fatalf("len(steps) = %d", len(steps))
	}
	if steps[1].Index != 2 || steps[2].Index != 3 || steps[3].Index != 4 || steps[4].Index != 5 || steps[5].Index != 6 {
		t.Fatalf("steps = %#v", steps)
	}
}
