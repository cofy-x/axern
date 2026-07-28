package localstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestCreateEpisodeLayoutWritesLayoutAndJSON(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), ".axrun", "runs"))
	runLayout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	result, err := store.CreateEpisodeLayout(runLayout, testTask("smoke-task"), testEpisode("test-run", "smoke-task"))
	if err != nil {
		t.Fatalf("CreateEpisodeLayout returned error: %v", err)
	}

	for _, path := range []string{result.TaskDir, result.EpisodeDir, result.ArtifactDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
	}
	for _, path := range []string{result.TaskJSONPath, result.EpisodeJSONPath, result.TrajectoryPath, result.AgentJSONPath, result.VerifierJSONPath, result.RewardJSONPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.IsDir() {
			t.Fatalf("%s is a directory", path)
		}
	}

	trajectory, err := os.ReadFile(result.TrajectoryPath)
	if err != nil {
		t.Fatalf("read trajectory.jsonl: %v", err)
	}
	if len(trajectory) != 0 {
		t.Fatalf("trajectory.jsonl length = %d, want empty", len(trajectory))
	}

	taskData, err := os.ReadFile(result.TaskJSONPath)
	if err != nil {
		t.Fatalf("read task.json: %v", err)
	}
	var task domain.TaskInstance
	if err := json.Unmarshal(taskData, &task); err != nil {
		t.Fatalf("decode task.json: %v", err)
	}
	if task.ID != "smoke-task" || task.Verifier.Type != "none" || task.Tags == nil {
		t.Fatalf("task = %#v", task)
	}

	episodeData, err := os.ReadFile(result.EpisodeJSONPath)
	if err != nil {
		t.Fatalf("read episode.json: %v", err)
	}
	var episode domain.Episode
	if err := json.Unmarshal(episodeData, &episode); err != nil {
		t.Fatalf("decode episode.json: %v", err)
	}
	if episode.ID != "episode_test-run_smoke-task_1" ||
		episode.TrajectoryPath != "episodes/episode_test-run_smoke-task_1/trajectory.jsonl" ||
		episode.AgentResultPath != "episodes/episode_test-run_smoke-task_1/agent.json" ||
		episode.VerifierResultPath != "episodes/episode_test-run_smoke-task_1/verifier.json" ||
		episode.RewardPath != "episodes/episode_test-run_smoke-task_1/reward.json" ||
		episode.ArtifactDir != "episodes/episode_test-run_smoke-task_1/artifacts" {
		t.Fatalf("episode = %#v, result = %#v", episode, result)
	}

	var agent domain.AgentResult
	if err := readJSON(result.AgentJSONPath, &agent); err != nil {
		t.Fatalf("decode agent.json: %v", err)
	}
	if agent.Status != domain.AgentStatusPending {
		t.Fatalf("agent = %#v", agent)
	}

	var verifier domain.VerifierResult
	if err := readJSON(result.VerifierJSONPath, &verifier); err != nil {
		t.Fatalf("decode verifier.json: %v", err)
	}
	if verifier.Status != domain.EpisodeStatusPending {
		t.Fatalf("verifier = %#v", verifier)
	}
	var reward domain.Reward
	if err := readJSON(result.RewardJSONPath, &reward); err != nil {
		t.Fatalf("decode reward.json: %v", err)
	}
	if reward.Status != domain.RewardStatusPending || reward.Score != nil {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestCreateEpisodeLayoutAllowsMultipleAttemptsForOneTask(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), ".axrun", "runs"))
	runLayout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	task := testTask("smoke-task")
	first, err := store.CreateEpisodeLayout(runLayout, task, testEpisodeAttempt("test-run", "smoke-task", 1))
	if err != nil {
		t.Fatalf("CreateEpisodeLayout first returned error: %v", err)
	}
	second, err := store.CreateEpisodeLayout(runLayout, task, testEpisodeAttempt("test-run", "smoke-task", 2))
	if err != nil {
		t.Fatalf("CreateEpisodeLayout second returned error: %v", err)
	}
	if first.TaskJSONPath != second.TaskJSONPath {
		t.Fatalf("task paths = %q and %q", first.TaskJSONPath, second.TaskJSONPath)
	}
	if first.EpisodeJSONPath == second.EpisodeJSONPath {
		t.Fatalf("episode paths should differ: %q", first.EpisodeJSONPath)
	}
	var episode domain.Episode
	if err := readJSON(second.EpisodeJSONPath, &episode); err != nil {
		t.Fatalf("decode episode.json: %v", err)
	}
	if episode.AttemptIndex != 2 {
		t.Fatalf("episode = %#v", episode)
	}
}

func TestCreateEpisodeLayoutRejectsConflictingTaskRecord(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), ".axrun", "runs"))
	runLayout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	task := testTask("smoke-task")
	if _, err := store.CreateEpisodeLayout(runLayout, task, testEpisodeAttempt("test-run", "smoke-task", 1)); err != nil {
		t.Fatalf("CreateEpisodeLayout first returned error: %v", err)
	}
	task.Instruction = "Different instruction"
	if _, err := store.CreateEpisodeLayout(runLayout, task, testEpisodeAttempt("test-run", "smoke-task", 2)); err == nil {
		t.Fatal("CreateEpisodeLayout error = nil, want conflicting task record error")
	}
}

func TestResetEpisodeSidecarsForResumeOverwritesStaleSidecars(t *testing.T) {
	store, layout := createEpisodeLayout(t)

	// Simulate a prior run: write non-pending episode + sidecar states and an artifact.
	startedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	if err := store.WriteEpisode(layout.EpisodeJSONPath, domain.Episode{
		ID:        layout.Episode.ID,
		RunID:     layout.Episode.RunID,
		TaskID:    layout.Episode.TaskID,
		Status:    domain.EpisodeStatusRunning,
		StartedAt: &startedAt,
	}); err != nil {
		t.Fatalf("WriteEpisode (stale): %v", err)
	}
	if err := store.WriteAgentResult(layout.AgentJSONPath, domain.AgentResult{
		Status:  domain.AgentStatusCompleted,
		Summary: "prior run output",
	}); err != nil {
		t.Fatalf("WriteAgentResult: %v", err)
	}
	if err := store.WriteVerifierResult(layout.VerifierJSONPath, domain.VerifierResult{
		Status: domain.EpisodeStatusCompleted,
		Type:   domain.VerifierTypeShell,
	}); err != nil {
		t.Fatalf("WriteVerifierResult: %v", err)
	}
	if err := store.WriteReward(layout.RewardJSONPath, domain.Reward{
		Status: domain.RewardStatusScored,
	}); err != nil {
		t.Fatalf("WriteReward: %v", err)
	}
	artifactPath := filepath.Join(layout.ArtifactDir, "stale-artifact.txt")
	if err := os.WriteFile(artifactPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale artifact: %v", err)
	}
	nestedArtifactPath := filepath.Join(layout.ArtifactDir, "llm", "request-000001.body")
	if err := os.MkdirAll(filepath.Dir(nestedArtifactPath), 0o755); err != nil {
		t.Fatalf("mkdir nested artifact dir: %v", err)
	}
	if err := os.WriteFile(nestedArtifactPath, []byte("stale nested"), 0o644); err != nil {
		t.Fatalf("write nested stale artifact: %v", err)
	}

	// layout.Episode mirrors the in-memory reset state (pending) as set by
	// resumableExecutions; the store function writes it back to disk.
	if err := store.ResetEpisodeSidecarsForResume(layout, domain.VerifierTypeShell); err != nil {
		t.Fatalf("ResetEpisodeSidecarsForResume: %v", err)
	}

	var episode domain.Episode
	if err := readJSON(layout.EpisodeJSONPath, &episode); err != nil {
		t.Fatalf("decode episode.json: %v", err)
	}
	if episode.Status != domain.EpisodeStatusPending {
		t.Fatalf("episode.Status = %q, want pending", episode.Status)
	}
	if episode.StartedAt != nil {
		t.Fatalf("episode.StartedAt = %v, want nil after reset", episode.StartedAt)
	}

	var agent domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agent); err != nil {
		t.Fatalf("decode agent.json: %v", err)
	}
	if agent.Status != domain.AgentStatusPending {
		t.Fatalf("agent.Status = %q, want pending", agent.Status)
	}

	var verifier domain.VerifierResult
	if err := readJSON(layout.VerifierJSONPath, &verifier); err != nil {
		t.Fatalf("decode verifier.json: %v", err)
	}
	if verifier.Status != domain.EpisodeStatusPending || verifier.Type != domain.VerifierTypeShell {
		t.Fatalf("verifier = %#v", verifier)
	}

	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("decode reward.json: %v", err)
	}
	if reward.Status != domain.RewardStatusPending {
		t.Fatalf("reward.Status = %q, want pending", reward.Status)
	}

	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("stale artifact still exists after reset: %v", err)
	}
	if _, err := os.Stat(nestedArtifactPath); !os.IsNotExist(err) {
		t.Fatalf("nested stale artifact still exists after reset: %v", err)
	}
}

func TestCreateEpisodeLayoutRejectsPathLikeIDs(t *testing.T) {
	store := New(t.TempDir())
	runLayout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	if _, err := store.CreateEpisodeLayout(runLayout, testTask("../escape"), testEpisode("test-run", "../escape")); err == nil {
		t.Fatal("CreateEpisodeLayout task error = nil, want path segment error")
	}
	if _, err := store.CreateEpisodeLayout(runLayout, testTask("smoke-task"), domain.Episode{ID: "nested/episode"}); err == nil {
		t.Fatal("CreateEpisodeLayout episode error = nil, want path segment error")
	}
}
