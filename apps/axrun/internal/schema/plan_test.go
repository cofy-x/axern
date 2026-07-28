package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunRejectsMissingRolloutPlan(t *testing.T) {
	runDir := createSchemaFixture(t)
	if err := os.Remove(filepath.Join(runDir, "plan.json")); err != nil {
		t.Fatalf("remove plan.json: %v", err)
	}
	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "", "read JSON") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsPlanEpisodeMismatch(t *testing.T) {
	runDir := createSchemaFixture(t)
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readSchemaJSON(t, planPath, &plan)
	plan.Episodes[0].TaskID = "missing-task"
	writeSchemaJSON(t, planPath, plan)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "episodes[0].task_id", "referenced task") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsPlanSelectionMismatch(t *testing.T) {
	runDir := createSchemaFixture(t)
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readSchemaJSON(t, planPath, &plan)
	plan.Selection.SelectedTaskCount = 99
	writeSchemaJSON(t, planPath, plan)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "selection.selected_task_count", "got 99, want 1") {
		t.Fatalf("result = %#v", result)
	}
}
