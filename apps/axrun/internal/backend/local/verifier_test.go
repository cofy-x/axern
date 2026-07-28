package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestLocalAdapterRejectsAgentImageRuntime(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.Episode.Agent = domain.AgentSpec{
		Name: "claude-code",
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "ghcr.io/cofy-x/claude-code:latest",
			Command: []string{"bash", "-lc", "true"},
		},
	}
	_, err := (Adapter{Now: fixedNow}).Execute(executeRequest(store, layout))
	if err == nil {
		t.Fatal("Execute error = nil, want agent-image rejection")
	}
	if !strings.Contains(err.Error(), "agent-image") || !strings.Contains(err.Error(), "local backend") {
		t.Fatalf("Execute error = %v", err)
	}
}

func TestRunnerPreflightValidatesClaudeCodeConfigWhenAgentEnabled(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "nope")
	err := (Adapter{AgentName: "claude-code", Registry: testRegistry()}).Preflight()
	if err == nil || !strings.Contains(err.Error(), "TIMEOUT_SEC") {
		t.Fatalf("Preflight error = %v, want config error", err)
	}
}

func TestRunnerPreflightIgnoresClaudeCodeEnvForOtherAgents(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "nope")
	if err := (Adapter{AgentName: "oracle", Registry: testRegistry()}).Preflight(); err != nil {
		t.Fatalf("Preflight returned error: %v", err)
	}
}

func TestRunnerExecuteNoneVerifierCompletesEpisode(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	episode, err := (Adapter{Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.Status != domain.EpisodeStatusCompleted || verifier.Type != "none" {
		t.Fatalf("verifier = %#v", verifier)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Status != domain.RewardStatusUnscored || reward.Score != nil || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
	if countTrajectorySteps(t, layout.TrajectoryPath) != 5 {
		t.Fatalf("trajectory step count mismatch")
	}
}

func TestRunnerExecuteShellVerifierRecordsOutput(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "printf hello"})
	episode, err := (Adapter{Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.ExitCode == nil || *verifier.ExitCode != 0 || verifier.Stdout != "hello" {
		t.Fatalf("verifier = %#v", verifier)
	}
}

func TestRunnerExecutesCommandAgentBeforeVerifier(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "grep -Fqx axrun-generic-job-ok answer.txt", CWD: "/workspace"})
	layout.Episode.Agent = domain.AgentSpec{Name: "command", Runtime: &domain.AgentRuntimeSpec{
		Type:    domain.AgentRuntimeTypeSandboxCommand,
		Command: []string{"/bin/sh", "-lc", "printf axrun-generic-job-ok > answer.txt && cat answer.txt"},
	}}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	layout.TaskInstance.InitialState = &domain.InitialStateSpec{Type: "directory", Path: workspace}
	layout.TaskInstance.Sandbox.Workdir = "/workspace"
	layout.Episode.Sandbox.Workdir = "/workspace"

	episode, err := (Adapter{Now: fixedNow, Registry: testRegistry()}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var agent domain.AgentResult
	readJSON(t, layout.AgentJSONPath, &agent)
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q agent=%#v verifier=%#v", episode.Status, agent, verifier)
	}
	if agent.Status != domain.AgentStatusCompleted || agent.Stdout != "axrun-generic-job-ok" {
		t.Fatalf("agent = %#v", agent)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Score == nil || *reward.Score != 1 {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestRunnerExecuteShellVerifierFailureRecordsRewardZero(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 7"})
	episode, err := (Adapter{Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed {
		t.Fatalf("episode status = %q", episode.Status)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.ExitCode == nil || *verifier.ExitCode != 7 || verifier.Error == "" {
		t.Fatalf("verifier = %#v", verifier)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Status != domain.RewardStatusScored || reward.Score == nil || *reward.Score != 0 || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
}
