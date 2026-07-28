package domain

// SummarizeEpisodes computes a RunSummary from a set of episodes,
// aggregating status counts, timing, usage, and cost.
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
		if episode.DurationMS > 0 {
			summary.TotalDurationMS += episode.DurationMS
			terminalCount++
		}
		if episode.Usage != nil {
			if summary.TotalUsage == nil {
				summary.TotalUsage = &UsageMetrics{}
			}
			summary.TotalUsage.InputTokens += episode.Usage.InputTokens
			summary.TotalUsage.OutputTokens += episode.Usage.OutputTokens
			summary.TotalUsage.TotalTokens += episode.Usage.TotalTokens
			summary.TotalUsage.ToolCalls += episode.Usage.ToolCalls
		}
		if episode.Cost != nil && episode.Cost.Amount > 0 {
			if summary.TotalCost == nil {
				summary.TotalCost = &CostMetrics{Currency: episode.Cost.Currency}
			}
			summary.TotalCost.Amount += episode.Cost.Amount
		}
	}
	if terminalCount > 0 {
		summary.MeanEpisodeDurationMS = summary.TotalDurationMS / int64(terminalCount)
	}
	return summary
}
