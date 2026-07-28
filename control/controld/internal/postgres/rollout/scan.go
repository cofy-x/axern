package pgrollout

import (
	"encoding/json"
	"fmt"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type rolloutRecord struct {
	rollout  *rolloutv1.Rollout
	specHash string
}

func rolloutSelectSQL() string {
	return `SELECT rollout_id, namespace, status, failure_class, spec, spec_hash, source_digest,
		descriptor_digest, plan_artifact_id, summary, message, labels, version,
		created_at, started_at, completed_at, deadline, start_policy, preflight FROM rollouts`
}

func scanRollout(row interface{ Scan(...any) error }) (*rolloutRecord, error) {
	var rollout rolloutv1.Rollout
	var statusText, failureText, startPolicyText string
	var specJSON, summaryJSON, labelsJSON, preflightJSON []byte
	var createdAt time.Time
	var startedAt, completedAt, deadline *time.Time
	var specHash string
	if err := row.Scan(&rollout.ID, &rollout.Namespace, &statusText, &failureText, &specJSON, &specHash,
		&rollout.SourceDigest, &rollout.DescriptorDigest, &rollout.PlanArtifactID,
		&summaryJSON, &rollout.Message, &labelsJSON, &rollout.Version,
		&createdAt, &startedAt, &completedAt, &deadline, &startPolicyText, &preflightJSON); err != nil {
		return nil, err
	}
	rollout.Status = parseRolloutStatus(statusText)
	rollout.FailureClass = parseFailureClass(failureText)
	if number, ok := rolloutv1.RolloutStartPolicy_value[startPolicyText]; ok {
		rollout.StartPolicy = rolloutv1.RolloutStartPolicy(number)
	}
	rollout.Spec = &rolloutv1.RolloutSpec{}
	if err := protojson.Unmarshal(specJSON, rollout.Spec); err != nil {
		return nil, fmt.Errorf("unmarshal rollout spec: %w", err)
	}
	rollout.Summary = &rolloutv1.RolloutSummary{}
	if err := protojson.Unmarshal(summaryJSON, rollout.Summary); err != nil {
		return nil, fmt.Errorf("unmarshal rollout summary: %w", err)
	}
	if err := json.Unmarshal(labelsJSON, &rollout.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal rollout labels: %w", err)
	}
	rollout.CreatedAt = timestamppb.New(createdAt)
	rollout.StartedAt = timeProto(startedAt)
	rollout.CompletedAt = timeProto(completedAt)
	rollout.Deadline = timeProto(deadline)
	if len(preflightJSON) > 0 {
		rollout.Preflight = &rolloutv1.PreflightReport{}
		if err := protojson.Unmarshal(preflightJSON, rollout.Preflight); err != nil {
			return nil, fmt.Errorf("unmarshal rollout preflight: %w", err)
		}
	}
	return &rolloutRecord{rollout: &rollout, specHash: specHash}, nil
}

func episodeSelectSQL() string {
	return `SELECT episode_id, rollout_id, task_id, task_digest, attempt_index,
		execution_generation, status, failure_class, passed, reward, input_tokens,
		cached_input_tokens, output_tokens, cost_microusd, duration_ms, execution_facts,
		artifact_manifest_id, message, created_at, started_at, completed_at
		FROM rollout_episodes`
}

func scanEpisode(row interface{ Scan(...any) error }) (*rolloutv1.Episode, error) {
	var episode rolloutv1.Episode
	var statusText, failureText string
	var factsJSON []byte
	var createdAt time.Time
	var startedAt, completedAt *time.Time
	if err := row.Scan(&episode.ID, &episode.RolloutID, &episode.TaskID, &episode.TaskDigest,
		&episode.AttemptIndex, &episode.ExecutionGeneration, &statusText, &failureText,
		&episode.Passed, &episode.Reward, &episode.InputTokens, &episode.CachedInputTokens,
		&episode.OutputTokens, &episode.CostMicrousd, &episode.DurationMs, &factsJSON,
		&episode.ArtifactManifestID, &episode.Message, &createdAt, &startedAt, &completedAt); err != nil {
		return nil, err
	}
	episode.Status = parseEpisodeStatus(statusText)
	episode.FailureClass = parseFailureClass(failureText)
	episode.ExecutionFacts = &rolloutv1.ExecutionFacts{}
	if err := protojson.Unmarshal(factsJSON, episode.ExecutionFacts); err != nil {
		return nil, fmt.Errorf("unmarshal execution facts: %w", err)
	}
	episode.CreatedAt = timestamppb.New(createdAt)
	episode.StartedAt = timeProto(startedAt)
	episode.CompletedAt = timeProto(completedAt)
	return &episode, nil
}

func parseRolloutStatus(value string) rolloutv1.RolloutStatus {
	if number, ok := rolloutv1.RolloutStatus_value[value]; ok {
		return rolloutv1.RolloutStatus(number)
	}
	return rolloutv1.RolloutStatus_ROLLOUT_STATUS_UNSPECIFIED
}

func parseEpisodeStatus(value string) rolloutv1.EpisodeStatus {
	if number, ok := rolloutv1.EpisodeStatus_value[value]; ok {
		return rolloutv1.EpisodeStatus(number)
	}
	return rolloutv1.EpisodeStatus_EPISODE_STATUS_UNSPECIFIED
}

func parseFailureClass(value string) rolloutv1.FailureClass {
	if number, ok := rolloutv1.FailureClass_value[value]; ok {
		return rolloutv1.FailureClass(number)
	}
	return rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED
}

func timeProto(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(value.UTC())
}
