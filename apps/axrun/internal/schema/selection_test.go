package schema

import (
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunAcceptsTaskSelection(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Selection = &domain.TaskSelection{
		RequestedTaskIDs:  []string{"task"},
		Limit:             1,
		ResolvedTaskCount: 2,
		SelectedTaskCount: 1,
	}
	writeSchemaJSON(t, runPath, run)
	writeSchemaJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Selection: domain.TaskSelection{
			RequestedTaskIDs:  []string{"task"},
			Limit:             1,
			ResolvedTaskCount: 2,
			SelectedTaskCount: 1,
		},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		Agent:           run.Agent,
		Provider:        schemaProviderRequirement(run.Agent),
		Model:           run.Model,
		Sandbox:         run.Sandbox,
		TaskIDs:         run.TaskIDs,
		Episodes: []domain.PlannedEpisode{{
			ID:           "episode_test-run_task_1",
			TaskID:       "task",
			AttemptIndex: 1,
			Order:        1,
		}},
	})

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
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Selection = &domain.TaskSelection{
		RequestedTaskIDs:  []string{"nested/task"},
		ResolvedTaskCount: 0,
		SelectedTaskCount: 99,
	}
	writeSchemaJSON(t, runPath, run)

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
