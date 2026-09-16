package domain

// SummarizeEpisodes computes lifecycle and timing aggregates from Episodes.
// Agent usage and cost are aggregated separately from AgentResult, their
// authoritative owner.
func SummarizeEpisodes(taskCount int, episodes []Episode) RunSummary {
	summary := RunSummary{
		TaskCount:    taskCount,
		EpisodeCount: len(episodes),
	}
	var terminalCount int
	for _, episode := range episodes {
		switch episode.Status {
		case EpisodeStatusPending:
			summary.PendingEpisodes++
		case EpisodeStatusRunning:
			summary.RunningEpisodes++
		case EpisodeStatusVerifying:
			summary.VerifyingEpisodes++
		case EpisodeStatusCompleted:
			summary.CompletedEpisodes++
		case EpisodeStatusFailed:
			summary.FailedEpisodes++
		}
		switch episode.FailureClass {
		case FailureClassAgentFailed:
			summary.AgentFailedEpisodes++
		case FailureClassVerifierFailed:
			summary.VerifierFailedEpisodes++
		case FailureClassInfrastructure:
			summary.InfraFailures++
		case FailureClassPatchEmpty:
			summary.PatchEmptyEpisodes++
		case FailureClassPatchInvalid:
			summary.PatchInvalidEpisodes++
		case FailureClassTimeout:
			summary.TimeoutEpisodes++
		}
		if episode.Timing != nil && episode.Timing.TotalMS > 0 {
			summary.TotalDurationMS += episode.Timing.TotalMS
			terminalCount++
		}
	}
	if terminalCount > 0 {
		summary.MeanEpisodeDurationMS = summary.TotalDurationMS / int64(terminalCount)
	}
	return summary
}

func AddAgentResults(summary *RunSummary, results []AgentResult) {
	for _, result := range results {
		if result.Usage != nil {
			if summary.TotalUsage == nil {
				summary.TotalUsage = &UsageMetrics{}
			}
			summary.TotalUsage.InputTokens += result.Usage.InputTokens
			summary.TotalUsage.OutputTokens += result.Usage.OutputTokens
			summary.TotalUsage.TotalTokens += result.Usage.TotalTokens
			summary.TotalUsage.ToolCalls += result.Usage.ToolCalls
		}
		if result.Cost != nil && result.Cost.Amount > 0 {
			if summary.TotalCost == nil {
				summary.TotalCost = &CostMetrics{Currency: result.Cost.Currency}
			}
			summary.TotalCost.Amount += result.Cost.Amount
		}
	}
}
