package localstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func createEpisodeLayout(t *testing.T) (Store, EpisodeLayout) {
	t.Helper()
	store := New(filepath.Join(t.TempDir(), "runs"))
	runLayout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	result, err := store.CreateEpisodeLayout(runLayout, testTask("smoke-task"), testEpisode("test-run", "smoke-task"))
	if err != nil {
		t.Fatalf("CreateEpisodeLayout returned error: %v", err)
	}
	return store, result
}

func testRun(id string) domain.RolloutRun {
	return domain.RolloutRun{
		ID:              id,
		SchemaVersion:   domain.LocalSchemaVersion,
		Status:          domain.RunStatusCreated,
		CreatedAt:       time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
		Agent:           domain.AgentSpec{Name: "claude-code"},
		Model:           domain.ModelSpec{ID: "anthropic/claude-haiku-4-5"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		AttemptsPerTask: 1,
	}
}

func testTask(id string) domain.TaskInstance {
	return domain.TaskInstance{
		ID:          id,
		Instruction: "Print hello from the sandbox",
		Sandbox:     domain.SandboxSpec{Backend: "axern"},
		Verifier:    domain.VerifierSpec{Type: "none"},
		Tags:        []string{},
	}
}

func testEpisode(runID string, taskID string) domain.Episode {
	return testEpisodeAttempt(runID, taskID, 1)
}

func testEpisodeAttempt(runID string, taskID string, attempt int) domain.Episode {
	return domain.Episode{
		ID:           domain.NewEpisodeID(runID, taskID, attempt),
		RunID:        runID,
		TaskID:       taskID,
		AttemptIndex: attempt,
		Status:       domain.EpisodeStatusPending,
		Agent:        domain.AgentSpec{Name: "claude-code"},
		Model:        domain.ModelSpec{ID: "anthropic/claude-haiku-4-5"},
		Sandbox:      domain.SandboxSpec{Backend: "axern"},
	}
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
