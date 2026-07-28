package resumepolicy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

func TestDecideExecutesTerminalEpisodeMissingManifest(t *testing.T) {
	layout := completedLayout(t)
	layout.Episode.ArtifactManifestPath = ""

	decision := Decide(layout)
	if decision.Action != ActionExecute || decision.Reason != ReasonTerminalMissingManifest {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecideSkipsCompleteTerminalEpisode(t *testing.T) {
	layout := completedLayout(t)
	manifestPath := filepath.Join(layout.ArtifactDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"episode_id":"episode_run_task_1","generated_at":"2026-05-22T12:00:00Z","entries":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	decision := Decide(layout)
	if decision.Action != ActionSkip || decision.Reason != ReasonTerminalComplete {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecideExecutesTerminalEpisodeWithEscapingManifestRef(t *testing.T) {
	layout := completedLayout(t)
	layout.Episode.ArtifactManifestPath = "../outside.json"

	decision := Decide(layout)
	if decision.Action != ActionExecute || decision.Reason != ReasonTerminalMissingManifest {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecideExecutesTerminalEpisodeMissingSidecar(t *testing.T) {
	layout := completedLayout(t)
	if err := os.Remove(layout.AgentJSONPath); err != nil {
		t.Fatalf("remove agent sidecar: %v", err)
	}

	decision := Decide(layout)
	if decision.Action != ActionExecute || decision.Reason != ReasonTerminalMissingSidecar {
		t.Fatalf("decision = %#v", decision)
	}
}

func completedLayout(t *testing.T) localstore.EpisodeLayout {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "run")
	episodeID := "episode_run_task_1"
	episodeDir := filepath.Join(runDir, "episodes", episodeID)
	artifactDir := filepath.Join(episodeDir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	agentPath := filepath.Join(episodeDir, "agent.json")
	verifierPath := filepath.Join(episodeDir, "verifier.json")
	rewardPath := filepath.Join(episodeDir, "reward.json")
	trajectoryPath := filepath.Join(episodeDir, "trajectory.jsonl")
	for _, path := range []string{agentPath, verifierPath, rewardPath, trajectoryPath} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write sidecar %s: %v", path, err)
		}
	}
	completedAt := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	return localstore.EpisodeLayout{
		EpisodeDir:       episodeDir,
		AgentJSONPath:    agentPath,
		VerifierJSONPath: verifierPath,
		RewardJSONPath:   rewardPath,
		TrajectoryPath:   trajectoryPath,
		ArtifactDir:      artifactDir,
		Episode: domain.Episode{
			ID:                   episodeID,
			TaskID:               "task",
			AttemptIndex:         1,
			Status:               domain.EpisodeStatusFailed,
			CompletedAt:          &completedAt,
			ArtifactManifestPath: "episodes/" + episodeID + "/artifacts/manifest.json",
		},
	}
}
