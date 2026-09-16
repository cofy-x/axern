package schema

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateRunSummary(problems *collector, runDir string, path string, run domain.RolloutRun, taskCount int, episodes []domain.Episode) {
	rel := displayPath(runDir, path)
	if run.Summary == nil {
		problems.add(rel, "summary", "is required")
		return
	}
	expected := domain.SummarizeEpisodes(taskCount, episodes)
	agentResults := make([]domain.AgentResult, 0, len(episodes))
	for _, episode := range episodes {
		agentPath := filepath.Join(runDir, "episodes", episode.ID, "agent.json")
		if result, ok := readJSON[domain.AgentResult](problems, runDir, agentPath); ok {
			agentResults = append(agentResults, result)
		}
	}
	domain.AddAgentResults(&expected, agentResults)
	compareSummaryField(problems, rel, "summary.task_count", run.Summary.TaskCount, expected.TaskCount)
	compareSummaryField(problems, rel, "summary.episode_count", run.Summary.EpisodeCount, expected.EpisodeCount)
	compareSummaryField(problems, rel, "summary.pending_episodes", run.Summary.PendingEpisodes, expected.PendingEpisodes)
	compareSummaryField(problems, rel, "summary.running_episodes", run.Summary.RunningEpisodes, expected.RunningEpisodes)
	compareSummaryField(problems, rel, "summary.verifying_episodes", run.Summary.VerifyingEpisodes, expected.VerifyingEpisodes)
	compareSummaryField(problems, rel, "summary.completed_episodes", run.Summary.CompletedEpisodes, expected.CompletedEpisodes)
	compareSummaryField(problems, rel, "summary.failed_episodes", run.Summary.FailedEpisodes, expected.FailedEpisodes)
	compareSummaryField(problems, rel, "summary.agent_failed_episodes", run.Summary.AgentFailedEpisodes, expected.AgentFailedEpisodes)
	compareSummaryField(problems, rel, "summary.verifier_failed_episodes", run.Summary.VerifierFailedEpisodes, expected.VerifierFailedEpisodes)
	compareSummaryField(problems, rel, "summary.infra_failures", run.Summary.InfraFailures, expected.InfraFailures)
	compareSummaryFieldInt64(problems, rel, "summary.total_duration_ms", run.Summary.TotalDurationMS, expected.TotalDurationMS)
	compareSummaryFieldInt64(problems, rel, "summary.mean_episode_duration_ms", run.Summary.MeanEpisodeDurationMS, expected.MeanEpisodeDurationMS)
	compareUsageAndCost(problems, rel, run.Summary, &expected)
	validateRunStatusLifecycle(problems, rel, run, expected)
}

func compareUsageAndCost(problems *collector, path string, got, want *domain.RunSummary) {
	if !reflect.DeepEqual(got.TotalUsage, want.TotalUsage) {
		problems.add(path, "summary.total_usage", fmt.Sprintf("got %#v, want %#v", got.TotalUsage, want.TotalUsage))
	}
	if !reflect.DeepEqual(got.TotalCost, want.TotalCost) {
		problems.add(path, "summary.total_cost", fmt.Sprintf("got %#v, want %#v", got.TotalCost, want.TotalCost))
	}
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

func validateRunStatusLifecycle(problems *collector, path string, run domain.RolloutRun, expected domain.RunSummary) {
	summary := run.Summary
	if summary == nil {
		return
	}
	if expected.InfraFailures > 0 {
		if run.Status != domain.RunStatusFailed {
			problems.add(path, "status", fmt.Sprintf("run with infrastructure failures must be %q", domain.RunStatusFailed))
		}
		return
	}
	inProgress := expected.PendingEpisodes + expected.RunningEpisodes + expected.VerifyingEpisodes
	switch run.Status {
	case domain.RunStatusCreated:
		if expected.EpisodeCount > 0 && expected.PendingEpisodes != expected.EpisodeCount {
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
