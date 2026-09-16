package schema

import (
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunAcceptsTaskSelection(t *testing.T) {
	runDir := createSchemaFixture(t)
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readSchemaJSON(t, planPath, &plan)
	plan.Selection = domain.TaskSelection{
		RequestedTaskIDs:  []string{"task"},
		Limit:             1,
		ResolvedTaskCount: 2,
		SelectedTaskCount: 1,
	}
	writeSchemaJSON(t, planPath, plan)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidTaskSelection(t *testing.T) {
	runDir := createSchemaFixture(t)
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readSchemaJSON(t, planPath, &plan)
	plan.Selection = domain.TaskSelection{
		RequestedTaskIDs:  []string{"nested/task"},
		ResolvedTaskCount: 0,
		SelectedTaskCount: 99,
	}
	writeSchemaJSON(t, planPath, plan)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "selection.requested_task_ids", "must be a single path segment") {
		t.Fatalf("result = %#v", result)
	}
	if !containsProblem(result, "selection.selected_task_count", "got 99, want 1") {
		t.Fatalf("result = %#v", result)
	}
	if !containsProblem(result, "selection.resolved_task_count", "must be greater than zero") {
		t.Fatalf("result = %#v", result)
	}
}
