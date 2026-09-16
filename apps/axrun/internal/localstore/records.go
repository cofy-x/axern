package localstore

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (s Store) WriteEpisode(path string, episode domain.Episode) error {
	if err := writeJSON(path, episode); err != nil {
		return fmt.Errorf("write episode.json: %w", err)
	}
	return nil
}

func (s Store) WriteRolloutRun(path string, run domain.RolloutRun) error {
	if err := writeJSON(path, run); err != nil {
		return fmt.Errorf("write run.json: %w", err)
	}
	return nil
}

func (s Store) WriteRolloutPlan(path string, plan domain.RolloutPlan) error {
	if err := writeJSONExclusive(path, plan); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}
	return nil
}

func (s Store) WriteAgentResult(path string, result domain.AgentResult) error {
	if err := writeJSON(path, result); err != nil {
		return fmt.Errorf("write agent.json: %w", err)
	}
	return nil
}

func (s Store) WriteVerifierResult(path string, result domain.VerifierResult) error {
	if err := writeJSON(path, result); err != nil {
		return fmt.Errorf("write verifier.json: %w", err)
	}
	return nil
}

func (s Store) WriteReward(path string, reward domain.Reward) error {
	if err := writeJSON(path, reward); err != nil {
		return fmt.Errorf("write reward.json: %w", err)
	}
	return nil
}
