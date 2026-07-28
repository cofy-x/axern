package reward

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestNormalizeNoneVerifierIsUnscored(t *testing.T) {
	reward := Normalize(domain.VerifierResult{Status: domain.EpisodeStatusCompleted, Type: "none"})
	if reward.Status != domain.RewardStatusUnscored || reward.Score != nil || reward.Passed != nil || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestAgentFailedIsFinalUnscoredReward(t *testing.T) {
	reward := AgentFailed("agent exited")
	if reward.Status != domain.RewardStatusAgentFailed || reward.Score != nil || reward.Passed != nil || reward.Reason == "" || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestNormalizeShellSuccessScoresOne(t *testing.T) {
	reward := Normalize(domain.VerifierResult{Status: domain.EpisodeStatusCompleted, Type: "shell"})
	if reward.Status != domain.RewardStatusScored || reward.Score == nil || *reward.Score != 1 || reward.Passed == nil || !*reward.Passed || !reward.Final {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestNormalizeShellFailureScoresZero(t *testing.T) {
	reward := Normalize(domain.VerifierResult{Status: domain.EpisodeStatusFailed, Type: "shell", Error: "command exited with status 5"})
	if reward.Status != domain.RewardStatusScored || reward.Score == nil || *reward.Score != 0 || reward.Passed == nil || *reward.Passed || reward.Reason == "" {
		t.Fatalf("reward = %#v", reward)
	}
}

func TestNormalizeCopiesVerifierMetrics(t *testing.T) {
	metrics := map[string]float64{"verifier_failed_count": 1}
	reward := Normalize(domain.VerifierResult{Status: domain.EpisodeStatusFailed, Type: "shell", Error: "failed", Metrics: metrics})
	if reward.Metrics["verifier_failed_count"] != 1 {
		t.Fatalf("reward metrics = %#v", reward.Metrics)
	}
	metrics["verifier_failed_count"] = 2
	if reward.Metrics["verifier_failed_count"] != 1 {
		t.Fatalf("reward metrics aliased verifier metrics: %#v", reward.Metrics)
	}
}

func TestNormalizeUnsupportedVerifierIsInvalid(t *testing.T) {
	reward := Normalize(domain.VerifierResult{Status: domain.EpisodeStatusFailed, Type: "python", Error: "unsupported verifier type"})
	if reward.Status != domain.RewardStatusInvalid || !reward.Invalid || reward.Score == nil || *reward.Score != 0 || reward.Passed == nil || *reward.Passed {
		t.Fatalf("reward = %#v", reward)
	}
}
