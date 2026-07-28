package managedworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

func (w Worker) episode(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string, config Config) error {
	var profiles map[string]agentprofile.Profile
	managedProfileKey := ""
	if work.GetRollout().GetSpec().GetAgent().GetName() != "command" {
		resolved, err := w.resolveAgentProfile(ctx, work, leaseToken)
		if err != nil {
			return err
		}
		managedProfileKey = resolved.Snapshot.GetProfile().GetID()
		profiles = map[string]agentprofile.Profile{managedProfileKey: resolved.Runtime}
	}
	leaseDigest := sha256.Sum256([]byte(leaseToken))
	reservationID := "usage-" + work.GetEpisodeID() + fmt.Sprintf("-g%d-c%s", work.GetExecutionGeneration(), hex.EncodeToString(leaseDigest[:8]))
	tokens, cost := reservationFor(work.GetRollout())
	reserved := managedProfileKey != ""
	usageSettled := false
	if reserved {
		if _, err := w.client.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{
			WorkID:          work.GetID(),
			LeaseToken:      leaseToken,
			ReservationID:   reservationID,
			MaxTokens:       tokens,
			MaxCostMicrousd: cost,
		}); err != nil {
			return err
		}
		defer func() {
			if usageSettled {
				return
			}
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer releaseCancel()
			_, _ = w.client.ReleaseUsage(releaseCtx, &workerrolloutv1.ReleaseUsageRequest{
				WorkID:        work.GetID(),
				LeaseToken:    leaseToken,
				ReservationID: reservationID,
			})
		}()
	}
	params, err := paramsFromWork(ctx, work, config)
	if err != nil {
		return err
	}
	if managedProfileKey != "" {
		// Managed execution must resolve the frozen profile supplied by controld.
		// Never allow the harness to fall back to a worker-local config file.
		params.AgentProfile = managedProfileKey
	}
	result, err := (approllout.Service{AgentRegistry: agentcatalog.RegistryWithProfiles(profiles)}).Run(params)
	if err != nil {
		return err
	}
	completed, err := approllout.ReadControlEpisode(result)
	if err != nil {
		return err
	}
	episode := episodeProto(work, completed)
	manifestID, err := w.uploadEvidence(ctx, work, leaseToken, result)
	if err != nil {
		return err
	}
	if reserved {
		if _, err := w.client.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{
			WorkID:            work.GetID(),
			LeaseToken:        leaseToken,
			ReservationID:     reservationID,
			InputTokens:       episode.GetInputTokens(),
			CachedInputTokens: episode.GetCachedInputTokens(),
			OutputTokens:      episode.GetOutputTokens(),
			CostMicrousd:      episode.GetCostMicrousd(),
		}); err != nil {
			return err
		}
		usageSettled = true
	}
	episode.ArtifactManifestID = manifestID
	data, err := json.Marshal(completed)
	if err != nil {
		return fmt.Errorf("marshal completed episode: %w", err)
	}
	sum := sha256.Sum256(data)
	_, err = w.client.CompleteEpisode(ctx, &workerrolloutv1.CompleteEpisodeRequest{
		WorkID:             work.GetID(),
		LeaseToken:         leaseToken,
		ResultDigest:       "sha256:" + hex.EncodeToString(sum[:]),
		Episode:            episode,
		UsageReservationID: reservationID,
	})
	return err
}

func reservationFor(rollout *rolloutv1.Rollout) (int64, int64) {
	budget := rollout.GetSpec().GetBudget()
	episodes := int64(max(1, rollout.GetSummary().GetEpisodeCount()))
	preflight := rollout.GetPreflight().GetUsage()
	tokens := int64(0)
	cost := int64(0)
	if budget.GetMaxTokens() > 0 {
		available := max(int64(0), budget.GetMaxTokens()-preflight.GetInputTokens()-preflight.GetOutputTokens())
		tokens = available / episodes
	}
	if budget.GetMaxCostMicrousd() > 0 {
		available := max(int64(0), budget.GetMaxCostMicrousd()-preflight.GetCostMicrousd())
		cost = available / episodes
	}
	return tokens, cost
}

func episodeProto(work *workerrolloutv1.WorkItem, value approllout.ControlEpisode) *rolloutv1.Episode {
	status := rolloutv1.EpisodeStatus_EPISODE_STATUS_COMPLETED
	if value.Episode.Status == domain.EpisodeStatusFailed {
		status = rolloutv1.EpisodeStatus_EPISODE_STATUS_FAILED
	}
	failure := rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED
	switch value.Episode.FailureClass {
	case domain.FailureClassAgentFailed, domain.FailureClassPatchEmpty, domain.FailureClassPatchInvalid:
		failure = rolloutv1.FailureClass_FAILURE_CLASS_AGENT
	case domain.FailureClassVerifierFailed:
		failure = rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER
	case domain.FailureClassInfrastructure, domain.FailureClassTimeout:
		failure = rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE
	}
	facts := &rolloutv1.ExecutionFacts{}
	if state := value.Episode.SandboxState; state != nil {
		facts.AllocationID = state.AllocationID
		facts.NodeID = state.NodeID
		facts.RuntimeClass = state.RuntimeClass
		facts.PayloadFormat = state.PayloadFormat
		facts.PayloadDigest = state.PayloadDigest
		facts.CacheHit = state.CacheHit
		facts.ImageResolveMs = state.ImageResolveMs
		facts.ImagePullMs = state.ImagePullMs
		facts.CowPrepareMs = state.CowPrepareMs
		facts.VerifierMaterializeMs = state.VerifierMaterializeMs
	}
	facts.AgentBundleDigest = work.GetRollout().GetPreflight().GetAgentBundleDigest()
	episode := &rolloutv1.Episode{
		ID:                  work.GetEpisodeID(),
		RolloutID:           work.GetRolloutID(),
		TaskID:              work.GetEpisode().GetTaskID(),
		TaskDigest:          work.GetEpisode().GetTaskDigest(),
		AttemptIndex:        work.GetEpisode().GetAttemptIndex(),
		ExecutionGeneration: work.GetExecutionGeneration(),
		Status:              status,
		FailureClass:        failure,
		DurationMs:          value.Episode.DurationMS,
		ExecutionFacts:      facts,
	}
	if value.Reward.Score != nil {
		episode.Reward = *value.Reward.Score
	}
	if value.Reward.Passed != nil {
		episode.Passed = *value.Reward.Passed
	}
	if value.Episode.Usage != nil {
		episode.InputTokens = int64(value.Episode.Usage.InputTokens)
		episode.OutputTokens = int64(value.Episode.Usage.OutputTokens)
	}
	if value.Episode.Cost != nil {
		episode.CostMicrousd = int64(math.Round(value.Episode.Cost.Amount * 1_000_000))
	}
	return episode
}
