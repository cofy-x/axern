package rollout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func readArtifactManifest(t *testing.T, artifactDir string) domain.ArtifactManifest {
	t.Helper()
	var manifest domain.ArtifactManifest
	if err := readJSON(filepath.Join(artifactDir, "manifest.json"), &manifest); err != nil {
		t.Fatalf("read artifact manifest: %v", err)
	}
	return manifest
}

func TestExecuteDownloadsConfiguredArtifacts(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	agentSpec := domain.AgentSpec{Runtime: &domain.AgentRuntimeSpec{
		Artifacts: &domain.ArtifactPolicySpec{OutputPaths: []string{"/tmp/axrun-output"}},
	}}
	sb := &fakeSandbox{}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Agent:          agentSpec,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sb.downloadRemotePath != "/tmp/axrun-output" {
		t.Fatalf("downloadRemotePath = %q", sb.downloadRemotePath)
	}
	manifest := readArtifactManifest(t, layout.ArtifactDir)
	if len(manifest.Entries) != 1 || !strings.Contains(manifest.Entries[0].Path, "downloads/tmp_axrun-output") {
		t.Fatalf("artifact manifest = %#v", manifest)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if steps[len(steps)-2].Type != domain.TrajectoryEventArtifactDownload {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestExecuteResolvesRelativeArtifactFromAgentWorkdir(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	agentSpec := domain.AgentSpec{Runtime: &domain.AgentRuntimeSpec{
		Workdir:   "/workspace/project",
		Artifacts: &domain.ArtifactPolicySpec{OutputPaths: []string{"results/answer.txt"}},
	}}
	sb := &fakeSandbox{downloadHook: func(_ string, localPath string) error {
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(localPath, []byte("answer\n"), 0o644)
	}}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Agent:          agentSpec,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sb.downloadRemotePath != "/workspace/project/results/answer.txt" {
		t.Fatalf("downloadRemotePath = %q", sb.downloadRemotePath)
	}
	manifest := readArtifactManifest(t, layout.ArtifactDir)
	if len(manifest.Entries) != 1 || manifest.Entries[0].Kind != domain.ArtifactKindDownloadedFile {
		t.Fatalf("artifact manifest = %#v", manifest)
	}
}

func TestResolveArtifactOutputPathRejectsWorkdirEscape(t *testing.T) {
	if _, err := resolveArtifactOutputPath("/workspace", "../secret"); err == nil {
		t.Fatal("expected workdir escape to be rejected")
	}
}

func TestExecuteAddsArtifactsForAgentResultRefs(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	episodeID := layout.Episode.ID
	stdoutRef := "episodes/" + episodeID + "/artifacts/agent.stdout.txt"
	stderrRef := "episodes/" + episodeID + "/artifacts/agent.stderr.txt"
	rawLogRef := "episodes/" + episodeID + "/artifacts/agent.raw.jsonl"
	harness := &recordingAgent{result: agent.Result{
		Status:    domain.AgentStatusCompleted,
		StdoutRef: stdoutRef,
		StderrRef: stderrRef,
		RawLogRef: rawLogRef,
	}}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	manifest := readArtifactManifest(t, layout.ArtifactDir)
	if len(manifest.Entries) != 3 {
		t.Fatalf("artifact manifest = %#v", manifest)
	}
	var result domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if !hasArtifact(result.Artifacts, stdoutRef, domain.ArtifactKindAgentStdout) ||
		!hasArtifact(result.Artifacts, stderrRef, domain.ArtifactKindAgentStderr) ||
		!hasArtifact(result.Artifacts, rawLogRef, domain.ArtifactKindAgentRawLog) {
		t.Fatalf("agent artifacts = %#v", result.Artifacts)
	}
}

func TestExecuteCapturesConfiguredAgentPatch(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	agentSpec := domain.AgentSpec{Runtime: &domain.AgentRuntimeSpec{
		Artifacts: &domain.ArtifactPolicySpec{PatchPath: "/tmp/solution.patch"},
	}}
	sb := &fakeSandbox{execHook: func(command sandbox.ExecCommand, _ sandbox.ExecOptions) (sandbox.ExecResult, error) {
		if strings.Contains(command.Shell(), "/tmp/solution.patch") {
			return sandbox.ExecResult{Stdout: "diff --git a/file b/file\n"}, nil
		}
		return sandbox.ExecResult{}, nil
	}}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Agent:          agentSpec,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	var result domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if result.PatchRef == "" || !hasArtifact(result.Artifacts, result.PatchRef, domain.ArtifactKindPatch) {
		t.Fatalf("agent result = %#v", result)
	}
	manifest := readArtifactManifest(t, layout.ArtifactDir)
	if len(manifest.Entries) == 0 || manifest.Entries[0].Kind != domain.ArtifactKindPatch {
		t.Fatalf("artifact manifest = %#v", manifest)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventPatchCreated) {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestExecuteFailsAgentWhenRequiredPatchIsMissing(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 0"})
	agentSpec := domain.AgentSpec{Runtime: &domain.AgentRuntimeSpec{
		Artifacts: &domain.ArtifactPolicySpec{
			PatchPath:     "/tmp/solution.patch",
			PatchRequired: true,
		},
	}}
	sb := &fakeSandbox{}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Agent:          agentSpec,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassPatchEmpty {
		t.Fatalf("episode = %#v", episode)
	}
	var result domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if result.Status != domain.AgentStatusFailed ||
		result.ExitReason != domain.AgentExitReasonCompletedNoPatch ||
		result.Error == "" {
		t.Fatalf("agent result = %#v", result)
	}
	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if reward.Status != domain.RewardStatusAgentFailed || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if hasStepType(steps, domain.TrajectoryEventVerifierPlanned) {
		t.Fatalf("verifier should be skipped when required patch is missing: %#v", steps)
	}
}

func TestExecuteAppendsRawEventsForCapturedAgentArtifacts(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	rawLogRef := "episodes/" + layout.Episode.ID + "/artifacts/agent.raw.jsonl"
	harness := &recordingAgent{result: agent.Result{
		Status:    domain.AgentStatusCompleted,
		Stdout:    "ok\n",
		Stderr:    "warn\n",
		RawLogRef: rawLogRef,
	}}
	if _, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "test",
	}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(layout.ArtifactDir, "agent.raw.jsonl"))
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("raw log lines = %#v", lines)
	}
	for index, line := range lines {
		var event domain.AgentRawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode raw event: %v", err)
		}
		if event.Type != domain.AgentRawEventArtifact || event.EventID == "" || event.ArtifactRef == "" {
			t.Fatalf("event[%d] = %#v", index, event)
		}
	}
}

func hasArtifact(artifacts []domain.ArtifactRef, path string, kind domain.ArtifactKind) bool {
	for _, artifact := range artifacts {
		if artifact.Path == path && artifact.Kind == kind {
			return true
		}
	}
	return false
}
