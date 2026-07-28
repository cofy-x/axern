package schema

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateRunSummary(problems *collector, runDir string, path string, run domain.RolloutRun, taskCount int, episodes []domain.Episode) {
	rel := displayPath(runDir, path)
	if run.Summary == nil {
		problems.add(rel, "summary", "is required")
		return
	}
	expected := domain.SummarizeEpisodes(taskCount, episodes)
	compareSummaryField(problems, rel, "summary.task_count", run.Summary.TaskCount, expected.TaskCount)
	compareSummaryField(problems, rel, "summary.episode_count", run.Summary.EpisodeCount, expected.EpisodeCount)
	compareSummaryField(problems, rel, "summary.pending_episodes", run.Summary.PendingEpisodes, expected.PendingEpisodes)
	compareSummaryField(problems, rel, "summary.running_episodes", run.Summary.RunningEpisodes, expected.RunningEpisodes)
	compareSummaryField(problems, rel, "summary.verifying_episodes", run.Summary.VerifyingEpisodes, expected.VerifyingEpisodes)
	compareSummaryField(problems, rel, "summary.completed_episodes", run.Summary.CompletedEpisodes, expected.CompletedEpisodes)
	compareSummaryField(problems, rel, "summary.failed_episodes", run.Summary.FailedEpisodes, expected.FailedEpisodes)
	compareSummaryField(problems, rel, "summary.agent_failed_episodes", run.Summary.AgentFailedEpisodes, expected.AgentFailedEpisodes)
	compareSummaryField(problems, rel, "summary.verifier_failed_episodes", run.Summary.VerifierFailedEpisodes, expected.VerifierFailedEpisodes)
	if run.Summary.InfraFailures < 0 {
		problems.add(rel, "summary.infra_failures", "must be greater than or equal to zero")
	}
	compareSummaryFieldInt64(problems, rel, "summary.total_duration_ms", run.Summary.TotalDurationMS, expected.TotalDurationMS)
	compareSummaryFieldInt64(problems, rel, "summary.mean_episode_duration_ms", run.Summary.MeanEpisodeDurationMS, expected.MeanEpisodeDurationMS)
	validateRunStatusLifecycle(problems, rel, run)
}

func compareSummaryField(problems *collector, path string, field string, got int, want int) {
	if got != want {
		problems.add(path, field, fmt.Sprintf("got %d, want %d", got, want))
	}
}

func compareSummaryFieldInt64(problems *collector, path string, field string, got int64, want int64) {
	if got != want {
		problems.add(path, field, fmt.Sprintf("got %d, want %d", got, want))
	}
}

func validateRunStatusLifecycle(problems *collector, path string, run domain.RolloutRun) {
	summary := run.Summary
	if summary == nil {
		return
	}
	if summary.InfraFailures > 0 {
		if run.Status != domain.RunStatusFailed {
			problems.add(path, "status", fmt.Sprintf("run with infrastructure failures must be %q", domain.RunStatusFailed))
		}
		return
	}
	if run.Status == domain.RunStatusFailed {
		problems.add(path, "status", "failed run requires at least one infrastructure failure")
	}
	inProgress := summary.PendingEpisodes + summary.RunningEpisodes + summary.VerifyingEpisodes
	switch run.Status {
	case domain.RunStatusCreated:
		if summary.EpisodeCount > 0 && summary.PendingEpisodes != summary.EpisodeCount {
			problems.add(path, "status", "created run requires all episodes to be pending")
		}
	case domain.RunStatusRunning:
		if inProgress == 0 {
			problems.add(path, "status", "running run requires at least one pending, running, or verifying episode")
		}
	case domain.RunStatusCompleted:
		if inProgress != 0 {
			problems.add(path, "status", "completed run cannot contain pending, running, or verifying episodes")
		}
	}
}
