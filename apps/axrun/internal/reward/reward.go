package reward

import (
	"maps"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func AgentFailed(reason string) domain.Reward {
	return domain.Reward{
		Status: domain.RewardStatusAgentFailed,
		Reason: reason,
		Final:  true,
	}
}

func InfraFailed(reason string) domain.Reward {
	return domain.Reward{
		Status: domain.RewardStatusInfraFailed,
		Reason: reason,
		Final:  true,
	}
}

func Normalize(verifierResult domain.VerifierResult) domain.Reward {
	switch verifierResult.Type {
	case domain.VerifierTypeNone:
		return domain.Reward{
			Status: domain.RewardStatusUnscored,
			Final:  true,
		}
	}

	if verifierResult.Status == domain.EpisodeStatusCompleted {
		score := 1.0
		passed := true
		return domain.Reward{
			Status:  domain.RewardStatusScored,
			Score:   &score,
			Passed:  &passed,
			Metrics: copyMetrics(verifierResult.Metrics),
			Final:   true,
		}
	}

	score := 0.0
	passed := false
	reward := domain.Reward{
		Status:  domain.RewardStatusScored,
		Score:   &score,
		Passed:  &passed,
		Reason:  verifierResult.Error,
		Metrics: copyMetrics(verifierResult.Metrics),
		Final:   true,
	}
	if verifierResult.Type != domain.VerifierTypeShell {
		reward.Status = domain.RewardStatusInvalid
		reward.Invalid = true
	}
	return reward
}

func copyMetrics(metrics map[string]float64) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	copied := make(map[string]float64, len(metrics))
	maps.Copy(copied, metrics)
	return copied
}
