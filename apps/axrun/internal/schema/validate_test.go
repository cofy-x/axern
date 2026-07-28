package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunAcceptsCompleteRun(t *testing.T) {
	runDir := createSchemaFixture(t)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() || result.RunID != "test-run" {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsPendingSkeleton(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodeID := "episode_test-run_task_1"
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	run := domain.RolloutRun{
		SchemaVersion:   domain.LocalSchemaVersion,
		ID:              "test-run",
		Status:          domain.RunStatusCreated,
		CreatedAt:       now,
		Agent:           domain.AgentSpec{Name: "noop"},
		Model:           domain.ModelSpec{ID: "model"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		Concurrency:     1,
		AttemptsPerTask: 1,
		TaskIDs:         []string{"task"},
		Summary:         &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, PendingEpisodes: 1},
		OutputPath:      runDir,
	}
	writeSchemaJSON(t, filepath.Join(runDir, "run.json"), run)
	writeSchemaPlan(t, runDir, run, []domain.PlannedEpisode{{
		ID:           episodeID,
		TaskID:       "task",
		AttemptIndex: 1,
		Order:        1,
	}})
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", episodeID, "episode.json"), domain.Episode{
		ID:                 episodeID,
		RunID:              "test-run",
		TaskID:             "task",
		AttemptIndex:       1,
		Status:             domain.EpisodeStatusPending,
		Agent:              domain.AgentSpec{Name: "noop"},
		Model:              domain.ModelSpec{ID: "model"},
		Sandbox:            domain.SandboxSpec{Backend: "axern"},
		TrajectoryPath:     "episodes/" + episodeID + "/trajectory.jsonl",
		AgentResultPath:    "episodes/" + episodeID + "/agent.json",
		VerifierResultPath: "episodes/" + episodeID + "/verifier.json",
		RewardPath:         "episodes/" + episodeID + "/reward.json",
		ArtifactDir:        "episodes/" + episodeID + "/artifacts",
	})
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", episodeID, "agent.json"), domain.AgentResult{Status: domain.AgentStatusPending})
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", episodeID, "verifier.json"), domain.VerifierResult{Status: domain.EpisodeStatusPending, Type: "none"})
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", episodeID, "reward.json"), domain.Reward{Status: domain.RewardStatusPending})
	if err := os.WriteFile(filepath.Join(runDir, "episodes", episodeID, "trajectory.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsCurrentDirectory(t *testing.T) {
	runDir := createSchemaFixture(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(runDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	result, err := ValidateRun(Params{RunDir: "."})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() || result.RunID != "test-run" {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsEscapingArtifactRef(t *testing.T) {
	runDir := createSchemaFixture(t)
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.Artifacts = append(agent.Artifacts, domain.ArtifactRef{Path: "../outside", Kind: domain.ArtifactKindAgentRawLog})
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "artifacts[2].path", "must not escape") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidArtifactSHA256(t *testing.T) {
	runDir := createSchemaFixture(t)
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.Artifacts[0].SHA256 = "not-a-digest"
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "artifacts[0].sha256", "64-character lowercase hex digest") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidArtifactMediaType(t *testing.T) {
	runDir := createSchemaFixture(t)
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.Artifacts[0].MediaType = "invalid"
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "artifacts[0].media_type", "valid media type") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidAgentRawEventType(t *testing.T) {
	runDir := createSchemaFixture(t)
	rawLogPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "artifacts", "agent.raw.jsonl")
	if err := os.WriteFile(rawLogPath, []byte(`{"event_id":"raw-000001","type":"bad.event"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "line 1.type", "unsupported agent raw event type") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsEscapingAgentRawRef(t *testing.T) {
	runDir := createSchemaFixture(t)
	rawLogPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "artifacts", "agent.raw.jsonl")
	if err := os.WriteFile(rawLogPath, []byte(`{"event_id":"raw-000001","type":"llm.request","body_ref":"../outside"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "line 1.body_ref", "must not escape") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidAgentRuntime(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Agent.Runtime = &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage, TimeoutSec: -1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() ||
		!containsProblem(result, "agent.runtime.image", "is required") ||
		!containsProblem(result, "agent.runtime.timeout_sec", "greater than or equal") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsSandboxCommandWithoutCommand(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Agent.Runtime = &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeSandboxCommand}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "agent.runtime.command", "requires command or entrypoint") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidAgentRuntimeCommandShape(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Agent.Runtime = &domain.AgentRuntimeSpec{
		Type:       domain.AgentRuntimeTypeSandboxCommand,
		Command:    []string{"bash", "-lc", "true"},
		Entrypoint: []string{"/bin/bash"},
		Args:       []string{"-lc", "true"},
	}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() ||
		!containsProblem(result, "agent.runtime.command", "mutually exclusive") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsAgentRuntimeArgsWithoutEntrypoint(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Agent.Runtime = &domain.AgentRuntimeSpec{
		Type:    domain.AgentRuntimeTypeSandboxCommand,
		Command: []string{"bash", "-lc", "true"},
		Args:    []string{"ignored"},
	}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "agent.runtime.args", "args require entrypoint") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidAgentExecutionMetadata(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Agent.Runtime = &domain.AgentRuntimeSpec{
		Type:           domain.AgentRuntimeTypeSandboxCommand,
		Command:        []string{"true"},
		MaxTurns:       -1,
		IdleTimeoutSec: -1,
		Prompt: &domain.PromptSpec{
			Source: "bad",
			Rounds: []domain.PromptRoundSpec{{Index: 0, Source: "also-bad"}},
		},
		Session: &domain.AgentSessionSpec{Mode: "bad"},
	}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() ||
		!containsProblem(result, "agent.runtime.max_turns", "greater than or equal") ||
		!containsProblem(result, "agent.runtime.idle_timeout_sec", "greater than or equal") ||
		!containsProblem(result, "agent.runtime.prompt.source", "unsupported") ||
		!containsProblem(result, "agent.runtime.prompt.rounds[0].index", "greater than or equal") ||
		!containsProblem(result, "agent.runtime.session.mode", "unsupported") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsMissingCapturedInputRefs(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Input = &domain.InputSpec{Type: domain.InputTypeTaskSet, Format: domain.InputFormatTaskSet, Path: "inputs/missing-taskset-descriptor.json"}
	writeSchemaJSON(t, runPath, run)

	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.Source = &domain.SourceRef{Type: domain.SourceTypeDir, Path: "inputs/missing-task-dir"}
	task.InitialState = &domain.InitialStateSpec{
		Type:       "directory",
		Path:       "inputs/missing-task-dir",
		Dockerfile: "inputs/missing-task-dir/Dockerfile",
	}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "input.path", "referenced path does not exist") {
		t.Fatalf("result = %#v", result)
	}
	if !containsProblem(result, "source.path", "referenced path does not exist") {
		t.Fatalf("result = %#v", result)
	}
	if !containsProblem(result, "initial_state.dockerfile", "referenced path does not exist") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsCapturedInputRefs(t *testing.T) {
	runDir := createSchemaFixture(t)
	inputDir := filepath.Join(runDir, "inputs", "task-dir")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "inputs", "taskset-descriptor.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Input = &domain.InputSpec{Type: domain.InputTypeTaskSet, Format: domain.InputFormatTaskSet, Path: "inputs/taskset-descriptor.json"}
	writeSchemaJSON(t, runPath, run)
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readSchemaJSON(t, planPath, &plan)
	plan.Input = run.Input
	writeSchemaJSON(t, planPath, plan)

	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.Source = &domain.SourceRef{Type: domain.SourceTypeDir, Path: "inputs/task-dir"}
	task.InitialState = &domain.InitialStateSpec{Type: "directory", Path: "inputs/task-dir", Dockerfile: "inputs/task-dir/Dockerfile"}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsUnknownEnum(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var raw map[string]any
	readSchemaJSON(t, episodePath, &raw)
	raw["status"] = "mystery"
	writeSchemaJSON(t, episodePath, raw)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "status", "unsupported episode status") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsShellVerifierWithoutCommand(t *testing.T) {
	runDir := createSchemaFixture(t)
	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.Verifier = domain.VerifierSpec{Type: domain.VerifierTypeShell}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "verifier.command", "is required") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidTrajectoryEvent(t *testing.T) {
	runDir := createSchemaFixture(t)
	trajectoryPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "trajectory.jsonl")
	payload := []byte(`{"index":1,"timestamp":"2026-05-19T12:00:00Z","type":"agent.made_up","actor":"axrun","summary":"bad"}` + "\n")
	if err := os.WriteFile(trajectoryPath, payload, 0o644); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "line 1.type", "unsupported trajectory event type") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsMismatchedRunSummary(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 99}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "summary.completed_episodes", "got 99, want 1") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsEpisodeWithMissingTask(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.TaskID = "missing-task"
	writeSchemaJSON(t, episodePath, episode)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "task_id", "referenced task") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsTaskMissingFromRunTaskIDs(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.TaskIDs = nil
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "id", "task is missing from run.task_ids") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsCompletedRunWithFailedEpisode(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassVerifierFailed
	writeSchemaJSON(t, episodePath, episode)
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", "episode_test-run_task_1", "verifier.json"), domain.VerifierResult{
		Status: domain.EpisodeStatusFailed,
		Type:   domain.VerifierTypeShell,
		Error:  "command exited with status 1",
	})
	score := 0.0
	passed := false
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", "episode_test-run_task_1", "reward.json"), domain.Reward{
		Status: domain.RewardStatusScored,
		Score:  &score,
		Passed: &passed,
		Reason: "command exited with status 1",
		Final:  true,
	})
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Status = domain.RunStatusCompleted
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, FailedEpisodes: 1, VerifierFailedEpisodes: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsCompletedRunWithPendingEpisode(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Status = domain.RunStatusCompleted
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, PendingEpisodes: 1}
	writeSchemaJSON(t, runPath, run)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusPending
	episode.StartedAt = nil
	episode.FinishedAt = nil
	episode.CompletedAt = nil
	writeSchemaJSON(t, episodePath, episode)
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json"), domain.AgentResult{Status: domain.AgentStatusPending})
	writeSchemaJSON(
		t,
		filepath.Join(runDir, "episodes", "episode_test-run_task_1", "verifier.json"),
		domain.VerifierResult{
			Status: domain.EpisodeStatusPending,
			Type:   "none",
		},
	)
	writeSchemaJSON(t, filepath.Join(runDir, "episodes", "episode_test-run_task_1", "reward.json"), domain.Reward{Status: domain.RewardStatusPending})

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "status", "completed run cannot contain pending") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsNonFinalAgentResult(t *testing.T) {
	runDir := createSchemaFixture(t)
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.Status = domain.AgentStatusRunning
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "status", "agent result must be final") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsMountedBundleLauncherKind(t *testing.T) {
	runDir := createSchemaFixture(t)
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.LauncherKind = domain.AgentLauncherKindAgentImage
	agent.RuntimeType = domain.AgentRuntimeTypeAgentImage
	agent.RuntimeImage = "ghcr.io/cofy-x/claude-code:latest"
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil {
		t.Fatalf("ValidateRun returned error: %v", err)
	}
	if !result.Valid() {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInvalidAgentExitReason(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodeID := "episode_test-run_task_1"
	agentPath := filepath.Join(runDir, "episodes", episodeID, "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.ExitReason = "bad"
	writeSchemaJSON(t, agentPath, agent)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "exit_reason", "unsupported") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsFailedEpisodeWithoutFailureClass(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusFailed
	writeSchemaJSON(t, episodePath, episode)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, FailedEpisodes: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "failure_class", "failed episode requires a failure class") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInfrastructureFailureWithoutFailedRun(t *testing.T) {
	runDir := createSchemaFixture(t)
	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Status = domain.RunStatusCompleted
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 1, InfraFailures: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "status", "run with infrastructure failures") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInconsistentAgentFailure(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassAgentFailed
	writeSchemaJSON(t, episodePath, episode)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "failure_class", "agent_failed episode requires failed agent result") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsTimeoutFailureWithoutAgentFailedReward(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassTimeout
	writeSchemaJSON(t, episodePath, episode)

	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "agent.json")
	var agent domain.AgentResult
	readSchemaJSON(t, agentPath, &agent)
	agent.Status = domain.AgentStatusFailed
	writeSchemaJSON(t, agentPath, agent)

	rewardPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "reward.json")
	var reward domain.Reward
	readSchemaJSON(t, rewardPath, &reward)
	reward.Status = domain.RewardStatusInfraFailed
	writeSchemaJSON(t, rewardPath, reward)

	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Status = domain.RunStatusCompleted
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, FailedEpisodes: 1, TimeoutEpisodes: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "failure_class", "timeout episode requires agent_failed reward") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunRejectsInfrastructureFailureWithoutInfraReward(t *testing.T) {
	runDir := createSchemaFixture(t)
	episodePath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "episode.json")
	var episode domain.Episode
	readSchemaJSON(t, episodePath, &episode)
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassInfrastructure
	writeSchemaJSON(t, episodePath, episode)

	rewardPath := filepath.Join(runDir, "episodes", "episode_test-run_task_1", "reward.json")
	var reward domain.Reward
	readSchemaJSON(t, rewardPath, &reward)
	reward.Status = domain.RewardStatusAgentFailed
	writeSchemaJSON(t, rewardPath, reward)

	runPath := filepath.Join(runDir, "run.json")
	var run domain.RolloutRun
	readSchemaJSON(t, runPath, &run)
	run.Status = domain.RunStatusFailed
	run.Summary = &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, FailedEpisodes: 1, InfraFailures: 1}
	writeSchemaJSON(t, runPath, run)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "failure_class", "infrastructure episode requires infra_failed reward") {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateRunAcceptsRemoteTaskSetAssetPathsWithoutLocalFiles(t *testing.T) {
	runDir := createSchemaFixture(t)
	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.InitialState = &domain.InitialStateSpec{
		Type: "taskset_workspace_image",
		WorkspaceImage: &domain.WorkspaceImageSourceSpec{
			Variants: []domain.WorkspaceImageVariantSpec{{
				Format: "oci",
				Image:  "registry.example.com/tasksets/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			SourcePath: "tasks/task/workspace",
			Target:     "/workspace",
		},
	}
	task.Verifier = domain.VerifierSpec{
		Type:    domain.VerifierTypeShell,
		Command: "/workspace/.axrun/verifier/check.sh",
		Assets: []domain.VerifierAssetSpec{{
			Path:       "tasks/task/verifier/check.sh",
			TargetPath: "/workspace/.axrun/verifier/check.sh",
		}},
	}
	task.Oracle = &domain.OracleSpec{Path: "tasks/task/oracle/answer.txt"}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err != nil || !result.Valid() {
		t.Fatalf("ValidateRun() = (%#v, %v), want valid remote TaskSet asset paths", result, err)
	}
}

func TestValidateRunRejectsRemoteTaskSetAssetOutsideTaskPrefix(t *testing.T) {
	runDir := createSchemaFixture(t)
	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.InitialState = &domain.InitialStateSpec{
		Type: "taskset_workspace_image",
		WorkspaceImage: &domain.WorkspaceImageSourceSpec{
			Variants: []domain.WorkspaceImageVariantSpec{
				{
					Format: "oci",
					Image:  "registry.example.com/tasksets/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			},
			SourcePath: "tasks/task/workspace",
			Target:     "/workspace",
		},
	}
	task.Verifier = domain.VerifierSpec{
		Type:    domain.VerifierTypeShell,
		Command: "/workspace/.axrun/verifier/check.sh",
		Assets: []domain.VerifierAssetSpec{{
			Path:       "tasks/other/verifier/check.sh",
			TargetPath: "/workspace/.axrun/verifier/check.sh",
		}},
	}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil || result.Valid() || !containsProblem(result, "verifier.assets[0].path", "must be inside TaskSet payload prefix") {
		t.Fatalf("ValidateRun() = (%#v, %v), want TaskSet task-prefix error", result, err)
	}
}

func createSchemaFixture(t *testing.T) string {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "run")
	taskDir := filepath.Join(runDir, "tasks", "task")
	episodeID := "episode_test-run_task_1"
	episodeDir := filepath.Join(runDir, "episodes", episodeID)
	artifactDir := filepath.Join(episodeDir, "artifacts")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(artifactDir, "llm"), 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	score := 1.0
	passed := true
	run := domain.RolloutRun{
		SchemaVersion:   domain.LocalSchemaVersion,
		ID:              "test-run",
		Status:          domain.RunStatusCompleted,
		CreatedAt:       now,
		Agent:           domain.AgentSpec{Name: "noop"},
		Model:           domain.ModelSpec{ID: "model"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		Concurrency:     1,
		AttemptsPerTask: 1,
		TaskIDs:         []string{"task"},
		Summary:         &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 1},
		OutputPath:      runDir,
	}
	writeSchemaJSON(t, filepath.Join(runDir, "run.json"), run)
	writeSchemaPlan(t, runDir, run, []domain.PlannedEpisode{{
		ID:           episodeID,
		TaskID:       "task",
		AttemptIndex: 1,
		Order:        1,
	}})
	writeSchemaJSON(t, filepath.Join(taskDir, "task.json"), domain.TaskInstance{
		ID:          "task",
		Instruction: "Say ok",
		Sandbox:     domain.SandboxSpec{Backend: "axern"},
		Verifier:    domain.VerifierSpec{Type: "none"},
		Tags:        []string{},
	})
	writeSchemaJSON(t, filepath.Join(episodeDir, "episode.json"), domain.Episode{
		ID:                   episodeID,
		RunID:                "test-run",
		TaskID:               "task",
		AttemptIndex:         1,
		Status:               domain.EpisodeStatusCompleted,
		StartedAt:            &now,
		FinishedAt:           &now,
		CompletedAt:          &now,
		Agent:                domain.AgentSpec{Name: "noop"},
		Model:                domain.ModelSpec{ID: "model"},
		Sandbox:              domain.SandboxSpec{Backend: "axern"},
		TrajectoryPath:       "episodes/" + episodeID + "/trajectory.jsonl",
		AgentResultPath:      "episodes/" + episodeID + "/agent.json",
		VerifierResultPath:   "episodes/" + episodeID + "/verifier.json",
		RewardPath:           "episodes/" + episodeID + "/reward.json",
		ArtifactDir:          "episodes/" + episodeID + "/artifacts",
		ArtifactManifestPath: "episodes/" + episodeID + "/artifacts/manifest.json",
	})
	writeSchemaJSON(t, filepath.Join(episodeDir, "agent.json"), domain.AgentResult{
		Status:    domain.AgentStatusCompleted,
		Stdout:    "ok\n",
		RawLogRef: "episodes/" + episodeID + "/artifacts/agent.raw.jsonl",
		Artifacts: []domain.ArtifactRef{
			{Path: "episodes/" + episodeID + "/artifacts/agent.raw.jsonl", Kind: domain.ArtifactKindAgentRawLog, Role: domain.ArtifactRoleRaw},
			{Path: "episodes/" + episodeID + "/artifacts/llm", Kind: domain.ArtifactKindLLMTelemetry, Role: domain.ArtifactRoleRaw},
		},
	})
	writeSchemaJSON(t, filepath.Join(episodeDir, "verifier.json"), domain.VerifierResult{Status: domain.EpisodeStatusCompleted, Type: "none"})
	writeSchemaJSON(t, filepath.Join(episodeDir, "reward.json"), domain.Reward{Status: domain.RewardStatusScored, Score: &score, Passed: &passed, Final: true})
	trajectory := domain.TrajectoryStep{
		Index:     1,
		EventID:   "step-000001",
		Timestamp: now,
		Type:      domain.TrajectoryEventAgentFinished,
		Actor:     "claude-code",
		Summary:   "done",
	}
	data, err := json.Marshal(trajectory)
	if err != nil {
		t.Fatalf("marshal trajectory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(episodeDir, "trajectory.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "agent.raw.jsonl"), []byte(`{"event_id":"raw-000001","type":"agent.command_finished","timestamp":"2026-05-19T12:00:00Z","exit_code":0}`+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	writeSchemaJSON(t, filepath.Join(artifactDir, "manifest.json"), domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     episodeID,
		GeneratedAt:   now,
		Entries: []domain.ArtifactManifestEntry{
			{
				Kind:      domain.ArtifactKindAgentRawLog,
				Path:      "episodes/" + episodeID + "/artifacts/agent.raw.jsonl",
				Status:    domain.ArtifactManifestStatusPresent,
				MediaType: "application/x-ndjson",
				Role:      domain.ArtifactRoleRaw,
			},
			{
				Kind:   domain.ArtifactKindLLMTelemetry,
				Path:   "episodes/" + episodeID + "/artifacts/llm",
				Status: domain.ArtifactManifestStatusPresent,
				Role:   domain.ArtifactRoleRaw,
			},
		},
	})
	return runDir
}

func writeSchemaJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeSchemaPlan(t *testing.T, runDir string, run domain.RolloutRun, episodes []domain.PlannedEpisode) {
	t.Helper()
	writeSchemaJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Input:         run.Input,
		Selection: domain.TaskSelection{
			ResolvedTaskCount: len(run.TaskIDs),
			SelectedTaskCount: len(run.TaskIDs),
		},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		Agent:           run.Agent,
		Provider:        schemaProviderRequirement(run.Agent),
		Model:           run.Model,
		Sandbox:         run.Sandbox,
		TaskIDs:         append([]string(nil), run.TaskIDs...),
		Episodes:        episodes,
	})
}

func schemaProviderRequirement(agent domain.AgentSpec) *domain.ProviderRequirement {
	switch agent.Name {
	case "codex":
		return &domain.ProviderRequirement{WireAPI: "responses"}
	case "claude-code":
		return &domain.ProviderRequirement{WireAPI: "anthropic_messages"}
	default:
		return nil
	}
}

func readSchemaJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func containsProblem(result Result, field string, message string) bool {
	for _, problem := range result.Problems {
		if problem.Field == field && strings.Contains(problem.Message, message) {
			return true
		}
	}
	return false
}
