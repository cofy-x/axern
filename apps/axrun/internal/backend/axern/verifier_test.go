package axern

import (
	"errors"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestRunnerExecuteNoneVerifierCompletesWithoutExec(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "false")
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	runtime := &fakeRuntime{sandbox: &fakeSandbox{}}
	episode, err := (Adapter{Runtime: runtime, Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	baselineExecCalls := 1
	if runtime.sandbox.execCalls != baselineExecCalls {
		t.Fatalf("execCalls = %d, want %d (baseline only, verifier skipped)", runtime.sandbox.execCalls, baselineExecCalls)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Status != domain.RewardStatusUnscored || reward.Score != nil || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestRunnerExecuteShellVerifierSuccess(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "false")
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "printf ok", CWD: "/workspace", TimeoutSec: 9})
	runtime := &fakeRuntime{sandbox: &fakeSandbox{execResult: sandbox.ExecResult{Stdout: "ok"}}}
	episode, err := (Adapter{Runtime: runtime, Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode status = %q", episode.Status)
	}
	if runtime.sandbox.execCommand.Shell() != "printf ok" || runtime.sandbox.execOptions.CWD != "/workspace" || runtime.sandbox.execOptions.Timeout != 9*time.Second {
		t.Fatalf("exec command/options = %#v %#v", runtime.sandbox.execCommand, runtime.sandbox.execOptions)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.ExitCode == nil || *verifier.ExitCode != 0 || verifier.Stdout != "ok" {
		t.Fatalf("verifier = %#v", verifier)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Score == nil || *reward.Score != 1 {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestRunnerExecuteShellVerifierNonzeroFailsEpisodeWithoutError(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "false")
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "exit 5"})
	runtime := &fakeRuntime{sandbox: &fakeSandbox{execResult: sandbox.ExecResult{ExitCode: 5, Stderr: "nope"}}}
	episode, err := (Adapter{Runtime: runtime, Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusFailed {
		t.Fatalf("episode status = %q", episode.Status)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.ExitCode == nil || *verifier.ExitCode != 5 || verifier.Stderr != "nope" || verifier.Error == "" {
		t.Fatalf("verifier = %#v", verifier)
	}
	var reward domain.Reward
	readJSON(t, layout.RewardJSONPath, &reward)
	if reward.Score == nil || *reward.Score != 0 || reward.Status != domain.RewardStatusScored {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestRunnerExecuteExecErrorReturnsInfrastructureErrorBeforeVerifierWrite(t *testing.T) {
	t.Setenv("AXRUN_SANDBOX_HEALTH_ENABLED", "false")
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "true"})
	execErr := errors.New("exec unavailable")
	_, err := (Adapter{Runtime: &fakeRuntime{sandbox: &fakeSandbox{execErr: execErr}}, Now: fixedNow}).Execute(executeRequest(store, layout))
	if !errors.Is(err, execErr) {
		t.Fatalf("Execute error = %v, want %v", err, execErr)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.Status != domain.EpisodeStatusPending {
		t.Fatalf("verifier = %#v", verifier)
	}
}
