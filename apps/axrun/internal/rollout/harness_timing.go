package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// episodeTimer tracks wall-clock durations of each execution phase.
type episodeTimer struct {
	sandboxCreateMS   int64
	workspaceUploadMS int64
	agentExecMS       int64
	verifierExecMS    int64
}

func (t *episodeTimer) timing(startedAt time.Time, finishedAt time.Time) *domain.EpisodeTiming {
	totalMS := finishedAt.Sub(startedAt).Milliseconds()
	accounted := t.sandboxCreateMS + t.workspaceUploadMS + t.agentExecMS + t.verifierExecMS
	waitMS := max(totalMS-accounted, 0)
	return &domain.EpisodeTiming{
		SandboxCreateMS:   t.sandboxCreateMS,
		WorkspaceUploadMS: t.workspaceUploadMS,
		AgentExecMS:       t.agentExecMS,
		VerifierExecMS:    t.verifierExecMS,
		TotalMS:           totalMS,
		WaitMS:            waitMS,
	}
}

// finalizeEpisodeTiming records the phase breakdown and total wall-clock time.
func finalizeEpisodeTiming(episode *domain.Episode, timer *episodeTimer) {
	if episode.StartedAt == nil || episode.FinishedAt == nil {
		return
	}
	if timer != nil {
		episode.Timing = timer.timing(*episode.StartedAt, *episode.FinishedAt)
	}
}

// stampCompleted records when the terminal Episode lifecycle record is
// committed. Normal execution publishes its required sidecars first; recovery
// may commit an infrastructure terminal while preserving incomplete sidecars
// as crash evidence.
func stampCompleted(episode *domain.Episode, clock func() time.Time) {
	t := now(clock)
	episode.CompletedAt = &t
}

func now(clock func() time.Time) time.Time {
	if clock != nil {
		return clock().UTC()
	}
	return time.Now().UTC()
}
