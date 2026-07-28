package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunRejectsMissingEpisodeAttempt(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.AttemptsPerTask = 2
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "attempt_index", "missing episode for task") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsEpisodeAttemptOutOfRange(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.AttemptIndex = 2
	writeSchemaJSON(t, episodePath, episode)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "attempt_index", "want <= attempts_per_task 1") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsEpisodeIDMismatchingAttempt(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.ID = "episode_test-run_task_2"
	writeSchemaJSON(t, episodePath, episode)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "id", "want \"episode_test-run_task_1\"") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsMultipleAttempts(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.AttemptsPerTask = 2
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 2, CompletedEpisodes: 2}
	writeSchemaJSON(t, runPath, run)
	writeSchemaPlan(t, runDir, run, []domain.PlannedEpisode{
		{ID: "episode_test-run_task_1", TaskID: "task", AttemptIndex: 1, Order: 1},
		{ID: "episode_test-run_task_2", TaskID: "task", AttemptIndex: 2, Order: 2},
	})

	source := filepath.Join(runDir, "episodes", "episode_test-run_task_1")
	target := filepath.Join(runDir, "episodes", "episode_test-run_task_2")
	if err := copyDir(source, target); err != nil {
		t.Fatalf("copy episode: %v", err)
	}
	episodePath := filepath.Join(target, "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.ID = "episode_test-run_task_2"
	episode.AttemptIndex = 2
	episode.TrajectoryPath = "episodes/episode_test-run_task_2/trajectory.jsonl"
	episode.AgentResultPath = "episodes/episode_test-run_task_2/agent.json"
	episode.VerifierResultPath = "episodes/episode_test-run_task_2/verifier.json"
	episode.RewardPath = "episodes/episode_test-run_task_2/reward.json"
	episode.ArtifactDir = "episodes/episode_test-run_task_2/artifacts"
	episode.ArtifactManifestPath = "episodes/episode_test-run_task_2/artifacts/manifest.json"
	writeSchemaJSON(t, episodePath, episode)
	writeSchemaJSON(t, filepath.Join(target, "artifacts", "manifest.json"), domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     "episode_test-run_task_2",
		GeneratedAt:   *episode.CompletedAt,
		Entries:       []domain.ArtifactManifestEntry{},
	})

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func copyDir(source string, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
