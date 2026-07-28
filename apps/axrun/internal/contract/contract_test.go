package contract

import (
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidatePathSegment(t *testing.T) {
	for _, value := range []string{"task", "task-1", "task_1"} {
		if err := ValidatePathSegment("task id", value); err != nil {
			t.Fatalf("ValidatePathSegment(%q) returned error: %v", value, err)
		}
	}
	for _, value := range []string{"", ".", "..", "../task", "dir/task", `dir\task`} {
		if err := ValidatePathSegment("task id", value); err == nil {
			t.Fatalf("ValidatePathSegment(%q) error = nil", value)
		}
	}
	if err := ValidatePathSegment("task id", strings.Repeat("a", MaxPathSegmentBytes+1)); err == nil {
		t.Fatal("oversized path segment accepted")
	}
}

func TestValidateVerifierSpec(t *testing.T) {
	if err := ValidateVerifierSpec("task", domain.VerifierSpec{Type: domain.VerifierTypeNone}); err != nil {
		t.Fatalf("none verifier returned error: %v", err)
	}
	if err := ValidateVerifierSpec("task", domain.VerifierSpec{Type: domain.VerifierTypeShell, Command: "true"}); err != nil {
		t.Fatalf("shell verifier returned error: %v", err)
	}
	if err := ValidateVerifierSpec("task", domain.VerifierSpec{
		Type:    domain.VerifierTypeShell,
		Command: "bash run-tests.sh",
		Assets:  []domain.VerifierAssetSpec{{Path: "inputs/task/run-tests.sh", TargetPath: "/workspace/run-tests.sh"}},
	}); err != nil {
		t.Fatalf("shell verifier with assets returned error: %v", err)
	}
	for name, verifier := range map[string]domain.VerifierSpec{
		"none-with-command": {Type: domain.VerifierTypeNone, Command: "true"},
		"none-with-assets":  {Type: domain.VerifierTypeNone, Assets: []domain.VerifierAssetSpec{{Path: "inputs/task/run-tests.sh"}}},
		"shell-no-command":  {Type: domain.VerifierTypeShell},
		"negative-timeout":  {Type: domain.VerifierTypeShell, Command: "true", TimeoutSec: -1},
		"relative-target-path": {
			Type:    domain.VerifierTypeShell,
			Command: "true",
			Assets: []domain.VerifierAssetSpec{
				{Path: "inputs/task/run-tests.sh", TargetPath: "workspace/run-tests.sh"},
			},
		},
		"root-target-path": {
			Type:    domain.VerifierTypeShell,
			Command: "true",
			Assets: []domain.VerifierAssetSpec{
				{Path: "inputs/task/run-tests.sh", TargetPath: "/"},
			},
		},
		"empty-asset-path": {Type: domain.VerifierTypeShell, Command: "true", Assets: []domain.VerifierAssetSpec{{}}},
		"unknown":          {Type: "python"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateVerifierSpec("task", verifier); err == nil {
				t.Fatal("ValidateVerifierSpec error = nil")
			}
		})
	}
}

func TestValidateTimeoutPolicy(t *testing.T) {
	if err := ValidateTimeoutPolicy("task", &domain.TimeoutPolicy{AgentSec: 1, VerifierSec: 2, EpisodeSec: 3}); err != nil {
		t.Fatalf("ValidateTimeoutPolicy returned error: %v", err)
	}
	for name, timeouts := range map[string]domain.TimeoutPolicy{
		"agent":    {AgentSec: -1},
		"verifier": {VerifierSec: -1},
		"episode":  {EpisodeSec: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTimeoutPolicy("task", &timeouts); err == nil {
				t.Fatal("ValidateTimeoutPolicy error = nil")
			}
		})
	}
}

func TestValidateAgentRuntimeSpec(t *testing.T) {
	problems := ValidateAgentRuntimeSpec(&domain.AgentRuntimeSpec{
		Type:       domain.AgentRuntimeTypeSandboxCommand,
		Command:    []string{"bash", "-lc", "true"},
		Entrypoint: []string{"/bin/bash"},
		Args:       []string{"-lc", "true"},
		MaxTurns:   -1,
	})
	if !hasAgentRuntimeProblem(problems, "command", "mutually exclusive") ||
		!hasAgentRuntimeProblem(problems, "max_turns", "greater than or equal") {
		t.Fatalf("problems = %#v", problems)
	}
	problems = ValidateAgentRuntimeSpec(&domain.AgentRuntimeSpec{
		Type:    domain.AgentRuntimeTypeSandboxCommand,
		Command: []string{"bash", "-lc", "true"},
		Args:    []string{"unexpected"},
	})
	if !hasAgentRuntimeProblem(problems, "args", "args require entrypoint") {
		t.Fatalf("problems = %#v", problems)
	}
	problems = ValidateAgentRuntimeSpec(&domain.AgentRuntimeSpec{
		Type:        domain.AgentRuntimeTypeAgentImage,
		Image:       "axern/codex-bundle:dev",
		MountTarget: "/opt/axern",
		BinDir:      "/opt/axern/bin",
	})
	if !hasAgentRuntimeProblem(problems, "mount_target", "/opt/axern/agents") ||
		!hasAgentRuntimeProblem(problems, "bin_dir", "mount_target") {
		t.Fatalf("problems = %#v", problems)
	}
	problems = ValidateAgentRuntimeSpec(&domain.AgentRuntimeSpec{
		Type:        domain.AgentRuntimeTypeAgentImage,
		Image:       "axern/codex-bundle:dev",
		MountTarget: "/opt/axern/agents/codex",
		BinDir:      "/opt/axern/agents/codex/bin",
	})
	if len(problems) != 0 {
		t.Fatalf("problems = %#v", problems)
	}
}

func hasAgentRuntimeProblem(problems []AgentRuntimeProblem, field string, message string) bool {
	for _, problem := range problems {
		if problem.Field == field && strings.Contains(problem.Message, message) {
			return true
		}
	}
	return false
}

func TestControlledValues(t *testing.T) {
	if !IsRunStatus(domain.RunStatusCompleted) ||
		!IsEpisodeStatus(domain.EpisodeStatusCompleted) ||
		!IsAgentStatus(domain.AgentStatusCompleted) ||
		!IsAgentExitReason(domain.AgentExitReasonCompleted) ||
		!IsAgentExitReason(domain.AgentExitReasonTimeout) ||
		!IsFinalAgentStatus(domain.AgentStatusCompleted) ||
		!IsRewardStatus(domain.RewardStatusScored) ||
		!IsFailureClass(domain.FailureClassAgentFailed) ||
		!IsFailureClass(domain.FailureClassInfrastructure) ||
		!IsFailureClass(domain.FailureClassTimeout) ||
		!IsVerifierType(domain.VerifierTypeShell) ||
		!IsArtifactKind(domain.ArtifactKindLLMTelemetry) ||
		!IsArtifactKind(domain.ArtifactKindVerifierBreakdown) ||
		!IsArtifactRole(domain.ArtifactRoleRaw) ||
		!IsAgentRawEventType(domain.AgentRawEventLLMRequest) ||
		!IsTrajectoryEventType(domain.TrajectoryEventAgentLLMRequest) ||
		!IsTrajectoryEventType(domain.TrajectoryEventSystemResumeStarted) ||
		!IsTrajectoryEventType(domain.TrajectoryEventSystemHealthCheckFailed) ||
		!IsTrajectoryEventType(domain.TrajectoryEventSystemSandboxDeath) ||
		!IsTrajectoryEventType(domain.TrajectoryEventSystemInfraFailure) ||
		!IsTrajectoryEventType(domain.TrajectoryEventSystemTimeout) {
		t.Fatal("expected known controlled values to be valid")
	}
	if !IsAgentLauncherKind(domain.AgentLauncherKindSandboxCommand) ||
		!IsAgentLauncherKind(domain.AgentLauncherKindAgentImage) ||
		!IsAgentLauncherKind("") {
		t.Fatal("expected known agent launcher kinds to be valid")
	}
	if IsAgentLauncherKind("unknown-launcher") {
		t.Fatal("expected unknown agent launcher kind to be invalid")
	}
	if IsRunStatus("bad") ||
		IsEpisodeStatus("bad") ||
		IsAgentStatus("bad") ||
		IsAgentExitReason("bad") ||
		IsFinalAgentStatus(domain.AgentStatusRunning) ||
		IsRewardStatus("bad") ||
		IsFailureClass("bad") ||
		IsVerifierType("bad") ||
		IsArtifactKind("bad") ||
		IsArtifactRole("bad") ||
		IsAgentRawEventType("bad") ||
		IsTrajectoryEventType("bad") {
		t.Fatal("expected unknown controlled values to be invalid")
	}
}

func TestIsEpisodeCompleteRequiresFinishedAt(t *testing.T) {
	now := time.Now().UTC()
	episode := domain.Episode{
		Status:          domain.EpisodeStatusFailed,
		FinishedAt:      &now,
		CompletedAt:     &now,
		AgentResultPath: "episodes/e/agent.json",
		RewardPath:      "episodes/e/reward.json",
	}
	reward := domain.Reward{Final: true}
	if !IsEpisodeComplete(episode, reward) {
		t.Fatal("expected complete episode to pass completeness check")
	}

	episode.FinishedAt = nil
	if IsEpisodeComplete(episode, reward) {
		t.Fatal("expected missing finished_at episode to fail completeness check")
	}
}

func TestIsEpisodeExportReady(t *testing.T) {
	now := time.Now().UTC()
	episode := domain.Episode{
		Status:          domain.EpisodeStatusCompleted,
		FinishedAt:      &now,
		CompletedAt:     &now,
		AgentResultPath: "episodes/e/agent.json",
		RewardPath:      "episodes/e/reward.json",
	}
	reward := domain.Reward{Final: true}
	agent := domain.AgentResult{
		Status:    domain.AgentStatusCompleted,
		RawLogRef: "episodes/e/artifacts/agent.raw.jsonl",
		Artifacts: []domain.ArtifactRef{{Path: "episodes/e/artifacts/agent.raw.jsonl", Kind: domain.ArtifactKindAgentRawLog}},
	}
	if !IsEpisodeExportReady(episode, reward, agent) {
		t.Fatal("expected export-ready episode to pass")
	}

	agent.RawLogRef = "/tmp/agent.raw.jsonl"
	if IsEpisodeExportReady(episode, reward, agent) {
		t.Fatal("expected absolute raw log ref to fail export gate")
	}

	agent.RawLogRef = "episodes/e/artifacts/agent.raw.jsonl"
	agent.Artifacts = nil
	if IsEpisodeExportReady(episode, reward, agent) {
		t.Fatal("expected missing raw log artifact ref to fail export gate")
	}

	agent.Artifacts = []domain.ArtifactRef{{Path: "episodes/e/artifacts/agent.raw.jsonl", Kind: domain.ArtifactKindAgentRawLog}}
	agent.RawLogRef = ""
	agent.LLMRequestCount = 1
	if IsEpisodeExportReady(episode, reward, agent) {
		t.Fatal("expected llm telemetry without raw log ref to fail export gate")
	}
}
