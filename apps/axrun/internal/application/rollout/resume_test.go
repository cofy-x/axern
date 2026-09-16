package rollout

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

func TestFinalizeInterruptedEpisodePreservesEvidenceAndDoesNotReuseID(t *testing.T) {
	runDir := t.TempDir()
	episodeDir := filepath.Join(runDir, "episodes", "episode-run-task-1")
	artifactDir := filepath.Join(episodeDir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	interrupted := localstore.EpisodeLayout{
		EpisodeDir:       episodeDir,
		EpisodeJSONPath:  filepath.Join(episodeDir, "episode.json"),
		AgentJSONPath:    filepath.Join(episodeDir, "agent.json"),
		VerifierJSONPath: filepath.Join(episodeDir, "verifier.json"),
		RewardJSONPath:   filepath.Join(episodeDir, "reward.json"),
		TrajectoryPath:   filepath.Join(episodeDir, "trajectory.jsonl"),
		ArtifactDir:      artifactDir,
		Episode: domain.Episode{
			ID:           "episode-run-task-1",
			RunID:        "run",
			TaskID:       "task",
			AttemptIndex: 1,
			Status:       domain.EpisodeStatusRunning,
			StartedAt:    &startedAt,
		},
	}
	store := localstore.New(filepath.Dir(runDir))
	if err := store.WriteEpisode(interrupted.EpisodeJSONPath, interrupted.Episode); err != nil {
		t.Fatal(err)
	}
	evidence := map[string][]byte{
		interrupted.AgentJSONPath:                 []byte("agent crash evidence\n"),
		interrupted.VerifierJSONPath:              []byte("verifier crash evidence\n"),
		interrupted.RewardJSONPath:                []byte("reward crash evidence\n"),
		interrupted.TrajectoryPath:                []byte("trajectory crash evidence\n"),
		filepath.Join(artifactDir, "partial.log"): []byte("artifact crash evidence\n"),
	}
	for path, data := range evidence {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pending := interrupted
	pending.EpisodeDir = filepath.Join(runDir, "episodes", "episode-run-task-2")
	pending.EpisodeJSONPath = filepath.Join(pending.EpisodeDir, "episode.json")
	pending.Episode = domain.Episode{
		ID:           "episode-run-task-2",
		RunID:        "run",
		TaskID:       "task",
		AttemptIndex: 2,
		Status:       domain.EpisodeStatusPending,
	}
	loaded := localstore.LoadedRun{Episodes: []localstore.EpisodeLayout{interrupted, pending}}
	finishedAt := time.Date(2026, 9, 17, 8, 1, 0, 0, time.UTC)
	if err := finalizeInterruptedEpisodes(store, &loaded, func() time.Time { return finishedAt }); err != nil {
		t.Fatal(err)
	}
	got := loaded.Episodes[0].Episode
	if got.ID != interrupted.Episode.ID || got.Status != domain.EpisodeStatusFailed || got.FailureClass != domain.FailureClassInfrastructure {
		t.Fatalf("finalized episode = %#v", got)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(finishedAt) {
		t.Fatalf("completed_at = %v, want %v", got.CompletedAt, finishedAt)
	}
	if got.Timing != nil {
		t.Fatalf("interrupted episode timing = %#v, want no fabricated duration", got.Timing)
	}
	for path, want := range evidence {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("evidence %s changed: got %q want %q", path, got, want)
		}
	}
	executions := resumableExecutions(loaded.Episodes, domain.AgentSpec{Name: "noop"}, domain.ModelSpec{ID: "test"})
	if len(executions) != 1 || executions[0].Layout.Episode.ID != pending.Episode.ID {
		t.Fatalf("resumable executions = %#v, want only distinct pending attempt", executions)
	}
}
