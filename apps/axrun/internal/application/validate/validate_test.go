package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRunRejectsUnknownAgent(t *testing.T) {
	runDir := validationFixture(t)
	updateAgents(t, runDir, domain.AgentSpec{Name: "unknown-agent"})

	result, err := Run(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("Run error = nil")
	}
	if result.Valid() {
		t.Fatal("result.Valid() = true")
	}
	if !containsProblem(result, "plan.json", "agent", "unknown agent") {
		t.Fatalf("problems = %+v", result.Problems)
	}
}

func TestRunRejectsClaudeCodeAgentImageWithoutProfile(t *testing.T) {
	runDir := validationFixture(t)
	updateAgents(t, runDir, domain.AgentSpec{
		Name:           "claude-code",
		ApprovalPolicy: domain.AgentApprovalPolicyNever,
		Runtime: &domain.AgentRuntimeSpec{
			Type:      domain.AgentRuntimeTypeAgentImage,
			Image:     "example.com/claude-code-agent:dev",
			Artifacts: validClaudeCodeArtifacts(),
		},
	})

	result, err := Run(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("Run error = nil")
	}
	if !containsProblem(result, "plan.json", "agent.profile", "is required") {
		t.Fatalf("problems = %+v", result.Problems)
	}
}

func TestRunRejectsClaudeCodeAgentImageWithoutArtifactPolicy(t *testing.T) {
	runDir := validationFixture(t)
	updateAgents(t, runDir, domain.AgentSpec{
		Name:           "claude-code",
		Profile:        "deepseek",
		ApprovalPolicy: domain.AgentApprovalPolicyNever,
		Runtime: &domain.AgentRuntimeSpec{
			Type:  domain.AgentRuntimeTypeAgentImage,
			Image: "example.com/claude-code-agent:dev",
		},
	})

	result, err := Run(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("Run error = nil")
	}
	if !containsProblem(result, "plan.json", "agent", "requires artifact policy") {
		t.Fatalf("problems = %+v", result.Problems)
	}
}

func validationFixture(t *testing.T) string {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "run")
	episodeID := "episode_test-run_task_1"
	episodeDir := filepath.Join(runDir, "episodes", episodeID)
	taskDir := filepath.Join(runDir, "tasks", "task")
	if err := os.MkdirAll(filepath.Join(episodeDir, "artifacts"), 0o755); err != nil {
		t.Fatalf("mkdir episode: %v", err)
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task: %v", err)
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	score := 1.0
	passed := true
	run := domain.RolloutRun{
		SchemaVersion: domain.LocalSchemaVersion, ID: "test-run", Status: domain.RunStatusCompleted,
		CreatedAt: now, Summary: &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 1},
	}
	writeJSON(t, filepath.Join(runDir, "run.json"), run)
	writeJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Selection: domain.TaskSelection{
			ResolvedTaskCount: 1,
			SelectedTaskCount: 1,
		},
		Concurrency:     1,
		AttemptsPerTask: 1,
		Agent:           domain.AgentSpec{Name: "claude-code"},
		Provider:        validationProviderRequirement("claude-code", "anthropic", "anthropic_messages"),
		Model:           domain.ModelSpec{ID: "model"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		TaskIDs:         []string{"task"},
		Episodes: []domain.PlannedEpisode{{
			ID:           episodeID,
			TaskID:       "task",
			AttemptIndex: 1,
			Order:        1,
		}},
	})
	writeJSON(t, filepath.Join(taskDir, "task.json"), domain.TaskInstance{
		ID:          "task",
		Instruction: "Say ok",
		Sandbox:     domain.SandboxSpec{Backend: "axern"},
		Verifier:    domain.VerifierSpec{Type: "none"},
		Tags:        []string{},
	})
	writeJSON(t, filepath.Join(episodeDir, "episode.json"), domain.Episode{
		ID:           episodeID,
		RunID:        "test-run",
		TaskID:       "task",
		AttemptIndex: 1,
		Status:       domain.EpisodeStatusCompleted,
		StartedAt:    &now,
		FinishedAt:   &now,
		CompletedAt:  &now,
	})
	writeJSON(t, filepath.Join(episodeDir, "agent.json"), domain.AgentResult{Status: domain.AgentStatusCompleted})
	writeJSON(t, filepath.Join(episodeDir, "verifier.json"), domain.VerifierResult{Status: domain.EpisodeStatusCompleted, Type: "none"})
	writeJSON(t, filepath.Join(episodeDir, "reward.json"), domain.Reward{Status: domain.RewardStatusScored, Score: &score, Passed: &passed, Final: true})
	if err := os.WriteFile(filepath.Join(episodeDir, "trajectory.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}
	writeJSON(t, filepath.Join(episodeDir, "artifacts", "manifest.json"), domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     episodeID,
		GeneratedAt:   now,
		Entries:       []domain.ArtifactManifestEntry{},
	})
	return runDir
}

func updateAgents(t *testing.T, runDir string, spec domain.AgentSpec) {
	t.Helper()
	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	readJSONFile(t, planPath, &plan)
	plan.Agent = spec
	switch spec.Name {
	case "codex":
		plan.Provider = validationProviderRequirement("codex", "openai", "responses")
	case "claude-code":
		plan.Provider = validationProviderRequirement("claude-code", "anthropic", "anthropic_messages")
	default:
		plan.Provider = nil
	}
	writeJSON(t, planPath, plan)

}

func validationProviderRequirement(agent, provider, wireAPI string) *domain.ProviderRequirement {
	return &domain.ProviderRequirement{
		Agent: agent, Provider: provider, WireAPI: wireAPI,
		Endpoint: "https://api.example.test/v1", ConfigFingerprint: "sha256:" + strings.Repeat("0", 64),
	}
}

func validClaudeCodeArtifacts() *domain.ArtifactPolicySpec {
	return &domain.ArtifactPolicySpec{
		CaptureStdout: true,
		CaptureStderr: true,
		CaptureRawLog: true,
	}
}

func containsProblem(result Result, path string, field string, message string) bool {
	for _, problem := range result.Problems {
		if problem.Path == path && problem.Field == field && strings.Contains(problem.Message, message) {
			return true
		}
	}
	return false
}

func readJSONFile[T any](t *testing.T, path string, value *T) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if filepath.Base(path) == "plan.json" {
		compact, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(compact)
		runPath := filepath.Join(filepath.Dir(path), "run.json")
		var run domain.RolloutRun
		readJSONFile(t, runPath, &run)
		run.PlanPath = "plan.json"
		run.PlanDigest = fmt.Sprintf("sha256:%x", digest)
		runData, err := json.MarshalIndent(run, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(runPath, append(runData, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
