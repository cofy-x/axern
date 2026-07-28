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

// finalizeEpisodeTiming populates the episode's DurationMS and Timing
// fields from the accumulated timer and the episode's own timestamps.
func finalizeEpisodeTiming(episode *domain.Episode, timer *episodeTimer) {
	if episode.StartedAt == nil || episode.FinishedAt == nil {
		return
	}
	episode.DurationMS = episode.FinishedAt.Sub(*episode.StartedAt).Milliseconds()
	if timer != nil {
		episode.Timing = timer.timing(*episode.StartedAt, *episode.FinishedAt)
	}
}

// stampCompleted sets the CompletedAt timestamp as the atomic commit
// marker, signaling that all sidecar files have been written and the
// episode record is fully committed.
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
