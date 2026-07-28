package rollout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/reward"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
	"github.com/cofy-x/axern/apps/axrun/internal/verifier"
)

type verifierFlowRequest struct {
	store      Store
	paths      Paths
	task       domain.TaskInstance
	episode    domain.Episode
	sandbox    sandbox.Instance
	trajectory *trajectoryRecorder
	now        func() time.Time
}

type verifierFlowResult struct {
	Episode    domain.Episode
	DurationMS int64
}

func runVerifierFlow(ctx context.Context, request verifierFlowRequest) (verifierFlowResult, error) {
	episode := request.episode
	episode.Status = domain.EpisodeStatusVerifying
	if err := request.store.WriteEpisode(request.paths.EpisodeJSONPath, episode); err != nil {
		return verifierFlowResult{Episode: episode}, err
	}
	if err := uploadVerifierAssets(ctx, request.sandbox, request.paths, request.task, request.trajectory.append); err != nil {
		return verifierFlowResult{Episode: episode}, err
	}
	if _, err := request.trajectory.appendSummary(domain.TrajectoryEventVerifierPlanned, "rollout", fmt.Sprintf("verifier %q planned", request.task.Verifier.Type)); err != nil {
		return verifierFlowResult{Episode: episode}, err
	}

	result, verifyErr := verifier.Run(ctx, request.sandbox, request.task.Verifier)
	verifierDurationMS := result.DurationMS
	if verifyErr != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, verifyErr
	}
	taskOutputs, err := downloadTaskOutputs(ctx, request.sandbox, request.task, request.paths.ArtifactDir, request.trajectory.append)
	if err != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, err
	}
	if len(taskOutputs.MissingRequired) > 0 {
		result.Status = domain.EpisodeStatusFailed
		result.Error = "required TaskSet outputs were not produced: " + strings.Join(taskOutputs.MissingRequired, ", ")
	}
	rewardResult := reward.Normalize(result)
	episode.Status = result.Status
	finishedAt := now(request.now)
	episode.FinishedAt = &finishedAt
	if result.Status == domain.EpisodeStatusFailed && result.Error != "" {
		episode.FailureClass = domain.FailureClassVerifierFailed
	}
	if err := request.store.WriteVerifierResult(request.paths.VerifierJSONPath, result); err != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, err
	}
	if err := request.store.WriteReward(request.paths.RewardJSONPath, rewardResult); err != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, err
	}
	if _, err := request.trajectory.appendSummary(domain.TrajectoryEventVerifierFinished, "rollout", fmt.Sprintf("verifier finished with status %q", result.Status)); err != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, err
	}
	if len(result.Artifacts) > 0 {
		for _, artifact := range result.Artifacts {
			episode.Artifacts = appendArtifact(episode.Artifacts, artifact)
		}
	}
	artifacts, err := downloadConfiguredArtifacts(ctx, request.sandbox, episode.Agent, request.paths.ArtifactDir, request.trajectory.append)
	if err != nil {
		return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, err
	}
	if len(artifacts) > 0 {
		for _, artifact := range artifacts {
			episode.Artifacts = appendArtifact(episode.Artifacts, artifact)
		}
	}
	for _, artifact := range taskOutputs.Artifacts {
		episode.Artifacts = appendArtifact(episode.Artifacts, artifact)
	}
	return verifierFlowResult{Episode: episode, DurationMS: verifierDurationMS}, nil
}
