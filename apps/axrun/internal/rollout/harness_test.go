package rollout

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func mustURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestExecuteRunsAgentBeforeVerifier(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	harness := &recordingAgent{}
	episode, err := Execute(Request{
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
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	if episode.StartedAt == nil || episode.FinishedAt == nil {
		t.Fatalf("episode timestamps = %#v", episode)
	}
	if episode.SandboxState == nil || episode.SandboxState.AllocationID != "alloc-1" {
		t.Fatalf("episode sandbox state = %#v", episode.SandboxState)
	}
	if !harness.called || harness.request.Instruction != "Print hello" || harness.request.Agent.Name != "claude-code" {
		t.Fatalf("agent request = %#v", harness.request)
	}
	if harness.request.ArtifactDir != layout.ArtifactDir {
		t.Fatalf("artifact dir = %q, want %q", harness.request.ArtifactDir, layout.ArtifactDir)
	}
	var agentResult domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agentResult); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if agentResult.Status != domain.AgentStatusCompleted || agentResult.Summary == "" {
		t.Fatalf("agent result = %#v", agentResult)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if len(steps) != 8 {
		t.Fatalf("len(steps) = %d, want 8", len(steps))
	}
	if steps[0].EventID != "step-000001" || steps[4].EventID != "step-000005" {
		t.Fatalf("step event ids = %#v", steps)
	}
	if steps[2].Type != domain.TrajectoryEventAgentPlanned ||
		steps[3].Type != domain.TrajectoryEventAgentStarted ||
		steps[4].Type != domain.TrajectoryEventAgentFinished ||
		steps[5].Type != domain.TrajectoryEventVerifierPlanned {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestExecuteRejectsMissingSandboxRuntime(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	_, err := Execute(Request{
		Store:       store,
		Task:        layout.TaskInstance,
		Episode:     layout.Episode,
		Paths:       paths(layout),
		Now:         fixedNow,
		RuntimeName: "test",
	})
	if err == nil {
		t.Fatal("Execute error = nil, want sandbox runtime error")
	}
	if !strings.Contains(err.Error(), "sandbox runtime is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteRejectsMissingStore(t *testing.T) {
	_, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	_, err := Execute(Request{
		Store:          nil,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err == nil {
		t.Fatal("Execute error = nil, want store required error")
	}
	if !strings.Contains(err.Error(), "store is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteAddsLLMTelemetrySummarySteps(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	harness := &recordingAgent{result: agent.Result{
		Status:           domain.AgentStatusCompleted,
		Summary:          "agent done",
		LauncherKind:     domain.AgentLauncherKindAgentImage,
		RuntimeType:      domain.AgentRuntimeTypeAgentImage,
		RuntimeImage:     "axern/claude-code-bundle:dev",
		RuntimeProfile:   "deepseek",
		RawLogRef:        "episodes/episode_test-run_smoke-task_1/artifacts/agent.raw.jsonl",
		LLMRequestCount:  1,
		LLMResponseCount: 1,
		Usage:            &domain.UsageMetrics{InputTokens: 3, OutputTokens: 5, TotalTokens: 8},
		Artifacts: []domain.ArtifactRef{
			{Path: "episodes/episode_test-run_smoke-task_1/artifacts/agent.raw.jsonl", Kind: "agent_raw_log"},
			{Path: "episodes/episode_test-run_smoke-task_1/artifacts/llm", Kind: "llm_telemetry"},
		},
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
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventAgentLLMRequest) || !hasStepType(steps, domain.TrajectoryEventAgentLLMResponse) {
		t.Fatalf("steps = %#v", steps)
	}
	for _, step := range steps {
		if step.Type == domain.TrajectoryEventAgentLLMResponse {
			if step.OutputRef == "" || step.RawRef == "" || step.Usage == nil || step.Usage.TotalTokens != 8 || len(step.Artifacts) != 2 {
				t.Fatalf("llm response step = %#v", step)
			}
		}
		if step.Type == domain.TrajectoryEventAgentFinished {
			if step.Metadata["launcher_kind"] != string(domain.AgentLauncherKindAgentImage) ||
				step.Metadata["runtime_type"] != string(domain.AgentRuntimeTypeAgentImage) ||
				step.Metadata["runtime_image"] != "axern/claude-code-bundle:dev" ||
				step.Metadata["runtime_profile"] != "deepseek" {
				t.Fatalf("agent finished metadata = %#v", step.Metadata)
			}
		}
	}
}

func TestExecuteStopsWhenAgentFails(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 0"})
	harness := &recordingAgent{result: agent.Result{Status: domain.AgentStatusFailed, Summary: "agent failed"}}
	sandbox := &fakeSandbox{}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sandbox},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassAgentFailed {
		t.Fatalf("episode = %#v", episode)
	}
	baselineExecCalls := 1
	if sandbox.execCalls != baselineExecCalls {
		t.Fatalf("execCalls = %d, want %d (baseline only, verifier skipped)", sandbox.execCalls, baselineExecCalls)
	}
	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if reward.Status != domain.RewardStatusAgentFailed || reward.Reason == "" || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
	var agentResult domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agentResult); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if agentResult.Status != domain.AgentStatusFailed || agentResult.Summary != "agent failed" {
		t.Fatalf("agent result = %#v", agentResult)
	}
}

func TestExecuteClassifiesPatchValidationFailureAsPatchInvalid(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 0"})
	harness := &recordingAgent{result: agent.Result{
		Status:  domain.AgentStatusFailed,
		Summary: "patch invalid",
		PatchValidation: &domain.PatchValidation{
			Valid: false,
			Error: "patch parse failed",
		},
	}}
	episode, err := Execute(Request{
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
	if episode.FailureClass != domain.FailureClassPatchInvalid {
		t.Fatalf("failure class = %q, want patch_invalid", episode.FailureClass)
	}
	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if reward.Status != domain.RewardStatusAgentFailed {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestExecuteClassifiesSandboxDeathAsInfrastructureFailure(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	harness := sandboxDeathAgent{}
	episode, err := Execute(Request{
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
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassInfrastructure {
		t.Fatalf("episode = %#v", episode)
	}
	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if reward.Status != domain.RewardStatusInfraFailed || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventSystemInfraFailure) {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestExecuteHealthMonitorDeclaresSandboxDeathAsInfrastructureFailure(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	healthErr := &sandbox.SandboxDeathError{
		Reason: "allocation not found",
		Cause:  errors.New("sandbox gone"),
	}
	sb := &fakeSandbox{
		execHook: func(command sandbox.ExecCommand, _ sandbox.ExecOptions) (sandbox.ExecResult, error) {
			if command.Shell() == "/bin/true" {
				return sandbox.ExecResult{}, healthErr
			}
			return sandbox.ExecResult{}, nil
		},
	}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &slowRecordingAgent{delay: 3 * time.Second},
		Now:            fixedNow,
		RuntimeName:    "test",
		HealthCheck: HealthCheckConfig{
			Enabled:      true,
			Interval:     10 * time.Millisecond,
			Threshold:    3,
			ProbeTimeout: 5 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassInfrastructure {
		t.Fatalf("episode = %#v", episode)
	}
	var agentResult domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agentResult); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if agentResult.ExitReason != domain.AgentExitReasonInfrastructure {
		t.Fatalf("agent result = %#v", agentResult)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventSystemSandboxDeath) || !hasStepType(steps, domain.TrajectoryEventSystemInfraFailure) {
		t.Fatalf("steps = %#v", steps)
	}
	for _, step := range steps {
		if step.Type != domain.TrajectoryEventSystemSandboxDeath {
			continue
		}
		if step.Metadata["phase"] != "agent" || step.Metadata["fatal_reason"] != "allocation not found" {
			t.Fatalf("sandbox death metadata = %#v", step.Metadata)
		}
		break
	}
}

func TestExecuteRejectsNonFinalAgentStatus(t *testing.T) {
	for _, status := range []domain.AgentStatus{"", domain.AgentStatusPending, domain.AgentStatusRunning, "made_up"} {
		t.Run(string(status), func(t *testing.T) {
			store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 0"})
			sandbox := &fakeSandbox{}
			_, err := Execute(Request{
				Store:          store,
				Task:           layout.TaskInstance,
				Episode:        layout.Episode,
				Paths:          paths(layout),
				SandboxRuntime: fakeRuntime{sandbox: sandbox},
				AgentHarness:   badStatusAgent{status: status},
				Now:            fixedNow,
				RuntimeName:    "test",
			})
			if err == nil {
				t.Fatal("Execute error = nil, want invalid agent status error")
			}
			baselineExecCalls := 1
			if sandbox.execCalls != baselineExecCalls {
				t.Fatalf("execCalls = %d, want %d (baseline only, verifier skipped)", sandbox.execCalls, baselineExecCalls)
			}
		})
	}
}

type badStatusAgent struct {
	status domain.AgentStatus
}

func (a badStatusAgent) Preflight() error {
	return nil
}

func (a badStatusAgent) Run(context.Context, agent.Request) (agent.Result, error) {
	return agent.Result{Status: a.status}, nil
}

func TestExecuteVerifierFailureIsEpisodeFailureNotInfrastructureError(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 7"})
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{execResult: sandbox.ExecResult{ExitCode: 7, Stderr: "nope"}}},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassVerifierFailed {
		t.Fatalf("episode = %#v", episode)
	}
	var reward domain.Reward
	if err := readJSON(layout.RewardJSONPath, &reward); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if reward.Status != domain.RewardStatusScored || reward.Score == nil || *reward.Score != 0 || reward.Passed == nil || *reward.Passed {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestExecuteVerifierSandboxDeathBecomesInfrastructureFailure(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "echo verify"})
	sb := &fakeSandbox{
		execHook: func(command sandbox.ExecCommand, _ sandbox.ExecOptions) (sandbox.ExecResult, error) {
			if command.Shell() == "echo verify" {
				return sandbox.ExecResult{}, &sandbox.SandboxDeathError{
					Reason: "allocation not found",
					Cause:  errors.New("verifier exec failed"),
				}
			}
			return sandbox.ExecResult{}, nil
		},
	}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassInfrastructure {
		t.Fatalf("episode = %#v", episode)
	}
	var rewardRecord domain.Reward
	if err := readJSON(layout.RewardJSONPath, &rewardRecord); err != nil {
		t.Fatalf("read reward: %v", err)
	}
	if rewardRecord.Status != domain.RewardStatusInfraFailed {
		t.Fatalf("reward = %#v", rewardRecord)
	}
}

func TestExecuteCleanupErrorIsInfrastructureErrorAfterWritingResult(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	closeErr := errors.New("close failed")
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{closeErr: closeErr}},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if !errors.Is(err, closeErr) {
		t.Fatalf("Execute error = %v, want %v", err, closeErr)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode = %#v", episode)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventSystemCleanupFailed) {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestExecuteTimesOutAgentWithAgentSec(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.TaskInstance.Timeouts = &domain.TimeoutPolicy{AgentSec: 1}
	slowAgent := &slowRecordingAgent{delay: 3 * time.Second}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   slowAgent,
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed {
		t.Fatalf("episode status = %q, want failed", episode.Status)
	}
	if episode.FailureClass != domain.FailureClassTimeout {
		t.Fatalf("failure class = %q, want timeout", episode.FailureClass)
	}
	var agentResult domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agentResult); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if agentResult.ExitReason != domain.AgentExitReasonTimeout {
		t.Fatalf("exit reason = %q, want timeout", agentResult.ExitReason)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if !hasStepType(steps, domain.TrajectoryEventSystemTimeout) {
		t.Fatal("expected system.timeout trajectory step")
	}
}

func TestExecuteTimesOutWithEpisodeSec(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.TaskInstance.Timeouts = &domain.TimeoutPolicy{EpisodeSec: 1}
	slowAgent := &slowRecordingAgent{delay: 3 * time.Second}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   slowAgent,
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed {
		t.Fatalf("episode status = %q, want failed", episode.Status)
	}
	if episode.FailureClass != domain.FailureClassTimeout {
		t.Fatalf("failure class = %q, want timeout", episode.FailureClass)
	}
}

func TestExecuteCompletedEpisodeHasCompletedAt(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.CompletedAt == nil {
		t.Fatal("CompletedAt is nil on completed episode")
	}
	if episode.FinishedAt == nil {
		t.Fatal("FinishedAt is nil on completed episode")
	}
}

func TestExecuteFailedAgentEpisodeHasCompletedAt(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 0"})
	harness := &recordingAgent{result: agent.Result{Status: domain.AgentStatusFailed, Summary: "agent failed"}}
	episode, err := Execute(Request{
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
	if episode.Status != domain.EpisodeStatusFailed {
		t.Fatalf("episode status = %q, want failed", episode.Status)
	}
	if episode.CompletedAt == nil {
		t.Fatal("CompletedAt is nil on failed episode")
	}
}

func TestExecuteRejectsManagedProxyOnLocalRuntime(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.Episode.Agent.Profile = "deepseek"
	harness := &managedProxyAgent{profile: "deepseek"}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "local",
	})
	if err == nil {
		t.Fatal("Execute should fail when agent requires managed proxy telemetry on local runtime")
	}
}

func TestExecutePersistsAgentImageRuntimeMetadataFromHarnessResult(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.Episode.Agent = domain.AgentSpec{
		Name: "claude-code",
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "ghcr.io/cofy-x/claude-code:latest",
			Command: []string{"bash", "-lc", "echo ok"},
		},
	}
	harness := &recordingAgent{result: agent.Result{
		Status:       domain.AgentStatusCompleted,
		Summary:      "agent done via image launcher",
		LauncherKind: domain.AgentLauncherKindAgentImage,
		RuntimeType:  domain.AgentRuntimeTypeAgentImage,
		RuntimeImage: "ghcr.io/cofy-x/claude-code:latest",
	}}
	sb := &fakeSandbox{}
	episode, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "axern",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	if episode.CompletedAt == nil {
		t.Fatal("CompletedAt is nil")
	}
	var agentResult domain.AgentResult
	if err := readJSON(layout.AgentJSONPath, &agentResult); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if agentResult.LauncherKind != domain.AgentLauncherKindAgentImage {
		t.Fatalf("launcher kind = %q, want agent-image", agentResult.LauncherKind)
	}
	if agentResult.RuntimeType != domain.AgentRuntimeTypeAgentImage {
		t.Fatalf("runtime type = %q", agentResult.RuntimeType)
	}
	if agentResult.RuntimeImage != "ghcr.io/cofy-x/claude-code:latest" {
		t.Fatalf("runtime image = %q", agentResult.RuntimeImage)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	var finishedStep *domain.TrajectoryStep
	for i := range steps {
		if steps[i].Type == domain.TrajectoryEventAgentFinished {
			finishedStep = &steps[i]
			break
		}
	}
	if finishedStep == nil {
		t.Fatal("missing agent.finished trajectory step")
	}
	if finishedStep.Metadata["launcher_kind"] != string(domain.AgentLauncherKindAgentImage) {
		t.Fatalf("agent.finished metadata launcher_kind = %q", finishedStep.Metadata["launcher_kind"])
	}
	if finishedStep.Metadata["runtime_type"] != string(domain.AgentRuntimeTypeAgentImage) {
		t.Fatalf("agent.finished metadata runtime_type = %q", finishedStep.Metadata["runtime_type"])
	}
}

func TestExecuteAllowsAgentWithoutManagedProxyOnLocalRuntime(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	harness := &managedProxyAgent{profile: ""}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: &fakeSandbox{}},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "local",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

type slowRecordingAgent struct {
	delay time.Duration
}

func (a *slowRecordingAgent) Preflight() error {
	return nil
}

func (a *slowRecordingAgent) Run(ctx context.Context, _ agent.Request) (agent.Result, error) {
	select {
	case <-ctx.Done():
		return agent.Result{}, ctx.Err()
	case <-time.After(a.delay):
		return agent.Result{Status: domain.AgentStatusCompleted, Summary: "slow agent done"}, nil
	}
}

type managedProxyAgent struct {
	profile string
}

type sandboxDeathAgent struct{}

func (sandboxDeathAgent) Preflight() error {
	return nil
}

func (sandboxDeathAgent) Run(context.Context, agent.Request) (agent.Result, error) {
	return agent.Result{}, &sandbox.SandboxDeathError{
		Reason: "allocation not found",
		Cause:  errors.New("not found"),
	}
}

func (a *managedProxyAgent) Preflight() error {
	return nil
}

func (a *managedProxyAgent) Run(_ context.Context, _ agent.Request) (agent.Result, error) {
	return agent.Result{Status: domain.AgentStatusCompleted, Summary: "done"}, nil
}

func (a *managedProxyAgent) ManagedProxyConfig(_ domain.AgentSpec) (*agent.ManagedProxyConfig, error) {
	if a.profile == "" {
		return nil, nil
	}
	return &agent.ManagedProxyConfig{
		Upstream:     mustURL("https://api.example.test/v1"),
		Token:        "test-token",
		ProviderType: agent.ProviderAnthropic,
	}, nil
}

func hasStepType(steps []domain.TrajectoryStep, stepType domain.TrajectoryEventType) bool {
	for _, step := range steps {
		if step.Type == stepType {
			return true
		}
	}
	return false
}
