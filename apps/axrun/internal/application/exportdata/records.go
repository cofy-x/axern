package exportdata

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func buildRecords(bundle episodeBundle, format Format) ([]any, error) {
	if !contract.IsEpisodeExportReady(bundle.Episode, bundle.Reward, bundle.Agent) {
		return nil, nil
	}
	switch format {
	case FormatSFT:
		return []any{buildSFTRecord(bundle)}, nil
	case FormatReward:
		return []any{buildRewardRecord(bundle)}, nil
	case FormatTrace:
		records, err := buildTraceRecords(bundle)
		if err != nil {
			return nil, err
		}
		return records, nil
	default:
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
}

func buildSFTRecord(bundle episodeBundle) SFTRecord {
	usage, cost := episodeUsageCost(bundle)
	return SFTRecord{
		SchemaVersion:       sftSchemaVersion,
		RecordID:            exportRecordID(FormatSFT, bundle.Episode.ID, 0),
		SourceSchemaVersion: bundle.Run.SchemaVersion,
		RunID:               bundle.Run.ID,
		EpisodeID:           bundle.Episode.ID,
		TaskID:              bundle.Task.ID,
		AttemptIndex:        bundle.Episode.AttemptIndex,
		Agent:               agentSummary(bundle.Episode.Agent),
		Model:               bundle.Episode.Model,
		Instruction:         bundle.Task.Instruction,
		Assistant:           bundle.Agent.Stdout,
		EpisodeStatus:       bundle.Episode.Status,
		AgentStatus:         bundle.Agent.Status,
		AgentExitReason:     bundle.Agent.ExitReason,
		Reward:              rewardSummary(bundle.Reward),
		Usage:               usage,
		Cost:                cost,
		DurationMS:          bundle.Episode.DurationMS,
		Timing:              bundle.Episode.Timing,
		FinishedAt:          bundle.Episode.FinishedAt,
		Refs:                bundle.Refs,
		Metadata:            exportMetadata(bundle),
	}
}

func buildRewardRecord(bundle episodeBundle) RewardRecord {
	usage, cost := episodeUsageCost(bundle)
	return RewardRecord{
		SchemaVersion:       rewardSchemaVersion,
		RecordID:            exportRecordID(FormatReward, bundle.Episode.ID, 0),
		SourceSchemaVersion: bundle.Run.SchemaVersion,
		RunID:               bundle.Run.ID,
		EpisodeID:           bundle.Episode.ID,
		TaskID:              bundle.Task.ID,
		AttemptIndex:        bundle.Episode.AttemptIndex,
		Agent:               agentSummary(bundle.Episode.Agent),
		Model:               bundle.Episode.Model,
		Instruction:         bundle.Task.Instruction,
		EpisodeStatus:       bundle.Episode.Status,
		AgentStatus:         bundle.Agent.Status,
		AgentExitReason:     bundle.Agent.ExitReason,
		Verifier:            verifierSummary(bundle.Verifier),
		Reward:              rewardSummary(bundle.Reward),
		Usage:               usage,
		Cost:                cost,
		DurationMS:          bundle.Episode.DurationMS,
		Timing:              bundle.Episode.Timing,
		StartedAt:           bundle.Episode.StartedAt,
		FinishedAt:          bundle.Episode.FinishedAt,
		Refs:                bundle.Refs,
		Metadata:            exportMetadata(bundle),
	}
}

func rewardSummary(reward domain.Reward) RewardSummary {
	return RewardSummary{
		Status: reward.Status,
		Score:  reward.Score,
		Passed: reward.Passed,
		Final:  reward.Final,
		Reason: reward.Reason,
	}
}

func verifierSummary(verifier domain.VerifierResult) VerifierSummary {
	return VerifierSummary{
		Type:     verifier.Type,
		Status:   verifier.Status,
		ExitCode: verifier.ExitCode,
		Error:    verifier.Error,
	}
}

func exportRecordID(format Format, episodeID string, sequence int) string {
	if sequence <= 0 {
		return fmt.Sprintf("%s_%s", format, episodeID)
	}
	return fmt.Sprintf("%s_%s_%06d", format, episodeID, sequence)
}

func episodeUsageCost(bundle episodeBundle) (*domain.UsageMetrics, *domain.CostMetrics) {
	return bundle.Episode.Usage, bundle.Episode.Cost
}

func exportMetadata(bundle episodeBundle) domain.KeyValue {
	metadata := domain.KeyValue{
		"source": "axrun",
	}
	if bundle.Task.Source != nil {
		metadata["task_source_type"] = string(bundle.Task.Source.Type)
	}
	if bundle.Agent.RawLogRef != "" {
		metadata["has_raw_llm_log"] = "true"
	}
	return metadata
}
