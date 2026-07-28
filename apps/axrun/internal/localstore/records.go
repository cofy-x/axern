package localstore

import (
	"fmt"
	"os"
	"path/filepath"

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
	if err := writeJSON(path, plan); err != nil {
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

// ResetEpisodeSidecarsForResume resets episode.json, agent.json, verifier.json,
// and reward.json to their initial pending states so a re-running episode does
// not observe stale outputs from a prior interrupted attempt. episode.json is
// written first so that a crash mid-reset leaves the episode in a pending state
// that is safe to detect and resume again. trajectory.jsonl is left intact so
// the resume marker appended by the caller preserves the full execution history.
func (s Store) ResetEpisodeSidecarsForResume(layout EpisodeLayout, verifierType domain.VerifierType) error {
	// Write episode.json first: if we crash after this but before the sidecars
	// are reset, the next resume attempt sees a consistent pending episode.
	if err := s.WriteEpisode(layout.EpisodeJSONPath, layout.Episode); err != nil {
		return fmt.Errorf("reset episode.json for resume: %w", err)
	}
	if err := writeJSON(layout.AgentJSONPath, domain.AgentResult{Status: domain.AgentStatusPending}); err != nil {
		return fmt.Errorf("reset agent.json for resume: %w", err)
	}
	if err := writeJSON(layout.VerifierJSONPath, domain.VerifierResult{
		Status: domain.EpisodeStatusPending,
		Type:   verifierType,
	}); err != nil {
		return fmt.Errorf("reset verifier.json for resume: %w", err)
	}
	if err := writeJSON(layout.RewardJSONPath, domain.Reward{Status: domain.RewardStatusPending}); err != nil {
		return fmt.Errorf("reset reward.json for resume: %w", err)
	}
	// Remove stale artifact contents from the prior run so a resumed episode
	// starts with a clean artifact tree. Keep the artifacts root directory, but
	// delete every nested file/subdirectory under it.
	if err := removeArtifactFiles(layout.ArtifactDir); err != nil {
		return fmt.Errorf("reset artifacts for resume: %w", err)
	}
	return nil
}

func removeArtifactFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read artifacts directory: %w", err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove artifact entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}
