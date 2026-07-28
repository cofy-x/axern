package rollout

import (
	"context"
	"errors"
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/reward"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func isTimeoutErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

type failureOptions struct {
	Source           string
	WriteAgentResult bool
}

func (s *executionSession) failTimeout(options failureOptions) (err error) {
	episode := s.episode
	defer func() { s.episode = episode }()
	if options.Source == "" {
		options.Source = "episode"
	}
	summary := "episode timeout exceeded"
	if _, err := s.trajectory.append(domain.TrajectoryStep{
		Type:    domain.TrajectoryEventSystemTimeout,
		Actor:   "rollout",
		Summary: summary,
		Metadata: domain.KeyValue{
			"source": options.Source,
		},
	}); err != nil {
		return err
	}
	if options.WriteAgentResult {
		agentResult := domain.AgentResult{
			Status:     domain.AgentStatusFailed,
			ExitReason: domain.AgentExitReasonTimeout,
			Error:      summary,
		}
		if err := s.store.WriteAgentResult(s.paths.AgentJSONPath, agentResult); err != nil {
			return err
		}
	}
	if err := s.store.WriteReward(s.paths.RewardJSONPath, reward.AgentFailed(summary)); err != nil {
		return err
	}
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassTimeout
	finishedAt := now(s.request.Now)
	episode.FinishedAt = &finishedAt
	finalizeEpisodeTiming(&episode, &s.timer)
	stampCompleted(&episode, s.request.Now)
	episode, err = writeArtifactManifest(s.store, s.paths, episode, s.request.Now)
	if err != nil {
		return err
	}
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, episode); err != nil {
		return err
	}
	return nil
}

// failEarlyInfrastructure handles infrastructure failures that occur before or
// during sandbox creation and workspace upload, before the agent phase starts.
// It writes trajectory, agent result, reward, and a terminal episode record so
// resume can detect the completed failure without leaving the episode in running
// state. A nil sandbox means no health monitor is running yet.
func (s *executionSession) failEarlyInfrastructure(cause error, source string) (domain.Episode, error) {
	err := s.failInfrastructure(cause, failureOptions{Source: source, WriteAgentResult: true})
	return s.episode, err
}

func isSandboxDeathErr(monitor *sandbox.Monitor, err error) bool {
	if sandbox.IsSandboxDeath(err) {
		return true
	}
	if monitor == nil {
		return false
	}
	select {
	case <-monitor.Dead():
		return true
	default:
		return false
	}
}

func (s *executionSession) failInfrastructure(cause error, options failureOptions) (err error) {
	episode := s.episode
	defer func() { s.episode = episode }()
	if options.Source == "" {
		options.Source = "agent"
	}
	summary := fmt.Sprintf("sandbox infrastructure failure: %v", cause)
	if _, err := s.trajectory.append(domain.TrajectoryStep{
		Type:    domain.TrajectoryEventSystemInfraFailure,
		Actor:   "rollout",
		Summary: summary,
		Metadata: domain.KeyValue{
			"source": options.Source,
		},
	}); err != nil {
		return err
	}
	if options.WriteAgentResult {
		agentResult := domain.AgentResult{
			Status:     domain.AgentStatusFailed,
			ExitReason: domain.AgentExitReasonInfrastructure,
			Error:      summary,
		}
		if err := s.store.WriteAgentResult(s.paths.AgentJSONPath, agentResult); err != nil {
			return err
		}
	}
	if err := s.store.WriteReward(s.paths.RewardJSONPath, reward.InfraFailed(summary)); err != nil {
		return err
	}
	episode.Status = domain.EpisodeStatusFailed
	episode.FailureClass = domain.FailureClassInfrastructure
	finishedAt := now(s.request.Now)
	episode.FinishedAt = &finishedAt
	finalizeEpisodeTiming(&episode, &s.timer)
	stampCompleted(&episode, s.request.Now)
	episode, err = writeArtifactManifest(s.store, s.paths, episode, s.request.Now)
	if err != nil {
		return err
	}
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, episode); err != nil {
		return err
	}
	return nil
}
