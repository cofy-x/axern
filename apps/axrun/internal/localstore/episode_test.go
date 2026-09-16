package localstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	if episode.ID != "episode_test-run_smoke-task_1" {
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
