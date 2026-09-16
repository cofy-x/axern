package rollout

import (
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

func summarizeRun(taskCount int, layouts []localstore.EpisodeLayout) domain.RunSummary {
	summary := domain.SummarizeEpisodes(taskCount, episodesFromLayouts(layouts))
	results := make([]domain.AgentResult, 0, len(layouts))
	for _, layout := range layouts {
		results = append(results, layout.AgentResult)
	}
	domain.AddAgentResults(&summary, results)
	return summary
}

func runStatusForExecutions(layouts []localstore.EpisodeLayout) domain.RunStatus {
	return runStatusForEpisodes(episodesFromLayouts(layouts))
}

func runStatusForEpisodes(episodes []domain.Episode) domain.RunStatus {
	status := domain.RunStatusCompleted
	for _, episode := range episodes {
		if episode.FailureClass == domain.FailureClassInfrastructure {
			return domain.RunStatusFailed
		}
		switch episode.Status {
		case domain.EpisodeStatusPending, domain.EpisodeStatusRunning, domain.EpisodeStatusVerifying:
			status = domain.RunStatusRunning
		}
	}
	return status
}

func episodesFromLayouts(layouts []localstore.EpisodeLayout) []domain.Episode {
	episodes := make([]domain.Episode, 0, len(layouts))
	for _, layout := range layouts {
		episodes = append(episodes, layout.Episode)
	}
	return episodes
}
