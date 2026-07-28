package rollout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type executionSession struct {
	request     Request
	runtime     sandbox.Runtime
	store       Store
	task        domain.TaskInstance
	paths       Paths
	trajectory  *trajectoryRecorder
	runtimeName string
	timer       episodeTimer
	episode     domain.Episode
}

func prepareExecution(request Request) (*executionSession, error) {
	runtime := request.SandboxRuntime
	if runtime == nil {
		return nil, fmt.Errorf("sandbox runtime is required")
	}
	if request.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if err := runtime.Preflight(); err != nil {
		return nil, err
	}
	if request.AgentHarness != nil {
		if err := request.AgentHarness.Preflight(); err != nil {
			return nil, err
		}
	}
	if err := preflightCapabilities(request); err != nil {
		return nil, err
	}

	trajectory, err := newTrajectoryRecorder(request.Store, request.Paths.TrajectoryPath, request.Now)
	if err != nil {
		return nil, err
	}

	runtimeName := request.RuntimeName
	if runtimeName == "" {
		runtimeName = "sandbox"
	}

	return &executionSession{
		request:     request,
		runtime:     runtime,
		store:       request.Store,
		task:        request.Task,
		paths:       request.Paths,
		trajectory:  trajectory,
		runtimeName: runtimeName,
		episode:     request.Episode,
	}, nil
}

func (s *executionSession) startEpisode() error {
	startedAt := now(s.request.Now)
	s.episode.Status = domain.EpisodeStatusRunning
	s.episode.StartedAt = &startedAt
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, s.episode); err != nil {
		return err
	}
	if _, err := s.trajectory.appendSummary(domain.TrajectoryEventSystemSandboxStarting, "rollout", fmt.Sprintf("%s sandbox starting", s.runtimeName)); err != nil {
		return err
	}
	return nil
}

func (s *executionSession) createEpisodeContext() (context.Context, context.CancelFunc) {
	ctx := context.Background()
	cancel := func() {}
	if timeouts := s.task.Timeouts; timeouts != nil && timeouts.EpisodeSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeouts.EpisodeSec)*time.Second)
	}
	return ctx, cancel
}

func (s *executionSession) createSandbox(ctx context.Context) (sandbox.Instance, error) {
	sandboxStart := time.Now()
	instance, err := s.runtime.Create(ctx)
	s.timer.sandboxCreateMS = time.Since(sandboxStart).Milliseconds()
	if err != nil {
		return nil, err
	}
	return instance, nil
}

func (s *executionSession) cleanupSandbox(ctx context.Context, instance sandbox.Instance, runErr *error) {
	closeErr := instance.Close(ctx)
	var stepErr error
	if closeErr != nil {
		_, stepErr = s.trajectory.appendSummary(domain.TrajectoryEventSystemCleanupFailed, "rollout", fmt.Sprintf("%s sandbox cleanup failed: %v", s.runtimeName, closeErr))
	} else {
		_, stepErr = s.trajectory.appendSummary(domain.TrajectoryEventSystemCleanupComplete, "rollout", fmt.Sprintf("%s sandbox cleanup completed", s.runtimeName))
	}
	if *runErr == nil {
		if stepErr != nil {
			*runErr = stepErr
		} else if closeErr != nil {
			*runErr = closeErr
		}
	}
}

func (s *executionSession) recordSandboxStart(instance sandbox.Instance) error {
	if state, err := instance.State(); err == nil {
		s.episode.SandboxState = sandboxRuntimeState(state)
		if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, s.episode); err != nil {
			return err
		}
		if state.AllocationID != "" {
			if _, err := s.trajectory.appendSummary(domain.TrajectoryEventSystemSandboxStarted, "rollout", fmt.Sprintf("%s sandbox started allocation %s", s.runtimeName, state.AllocationID)); err != nil {
				return err
			}
			return nil
		}
	}
	if _, err := s.trajectory.appendSummary(domain.TrajectoryEventSystemSandboxStarted, "rollout", fmt.Sprintf("%s sandbox started", s.runtimeName)); err != nil {
		return err
	}
	return nil
}

func (s *executionSession) prepareWorkspace(ctx context.Context, instance sandbox.Instance) (*WorkspaceBaseline, error) {
	uploadStart := time.Now()
	if _, err := uploadInitialWorkspace(ctx, instance, s.paths, s.task, s.trajectory.append); err != nil {
		return nil, err
	}
	s.timer.workspaceUploadMS = time.Since(uploadStart).Milliseconds()

	workdir := resolveWorkdir(s.task)
	baseline, err := captureWorkspaceBaseline(ctx, instance, workdir)
	if err != nil {
		return nil, fmt.Errorf("capture workspace baseline: %w", err)
	}
	if baseline == nil {
		return nil, nil
	}

	s.episode.BaseRevision = baseline.Revision
	captureUntrackedHashes(ctx, instance, workdir, baseline)
	if _, err := s.trajectory.append(domain.TrajectoryStep{
		Type:     domain.TrajectoryEventSystemWorkspaceBaseline,
		Actor:    "rollout",
		Summary:  fmt.Sprintf("workspace baseline captured at %s", baseline.Revision),
		Metadata: baselineMetadata(baseline),
	}); err != nil {
		return nil, err
	}
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, s.episode); err != nil {
		return nil, err
	}
	return baseline, nil
}

func (s *executionSession) runAgentPhase(ctx context.Context, instance sandbox.Instance, baseline *WorkspaceBaseline) (bool, error) {
	var managedProxy *sandbox.ManagedProxyOptions
	recorder, err := createAgentRecorder(s.request)
	if err != nil {
		return false, err
	}
	if pc, ok := s.request.AgentHarness.(agent.ManagedProxyConfigurer); ok {
		managedProxy, err = resolveManagedProxy(pc, s.episode.Agent, recorder)
		if err != nil {
			return false, err
		}
	}

	agentCtx, monitor, cleanup := s.buildPhaseContext(ctx, instance, "agent", s.agentTimeoutSec())
	defer cleanup()

	result, err := runAgentFlow(agentCtx, agentFlowRequest{
		store:        s.store,
		paths:        s.paths,
		task:         s.task,
		episode:      s.episode,
		sandbox:      instance,
		harness:      s.request.AgentHarness,
		trajectory:   s.trajectory,
		now:          s.request.Now,
		managedProxy: managedProxy,
		recorder:     recorder,
		baseline:     baseline,
	})
	if err != nil {
		if isSandboxDeathErr(monitor, err) {
			cause := err
			if monitor != nil && monitor.Err() != nil {
				cause = monitor.Err()
			}
			failErr := s.failInfrastructure(cause, failureOptions{Source: "agent", WriteAgentResult: true})
			return false, failErr
		}
		if isTimeoutErr(err) {
			failErr := s.failTimeout(failureOptions{Source: "episode", WriteAgentResult: true})
			return false, failErr
		}
		return false, err
	}

	s.episode = result.Episode
	s.timer.agentExecMS = result.Metrics.DurationMS
	s.episode.Usage = result.Metrics.Usage
	s.episode.Cost = result.Metrics.Cost
	return result.ShouldVerify, nil
}

func (s *executionSession) buildPhaseContext(ctx context.Context, instance sandbox.Instance, phase string, timeoutSec int) (context.Context, *sandbox.Monitor, func()) {
	phaseCtx := ctx
	var monitor *sandbox.Monitor
	cleanup := func() {}

	if s.request.HealthCheck.Enabled {
		monitor = sandbox.NewMonitor(instance, sandbox.MonitorOptions{
			Interval:     s.request.HealthCheck.Interval,
			Threshold:    s.request.HealthCheck.Threshold,
			ProbeTimeout: s.request.HealthCheck.ProbeTimeout,
			OnFailure: func(event sandbox.HealthEvent) {
				summary := fmt.Sprintf("%s sandbox health check failed during %s phase (attempt %d): %v", s.runtimeName, phase, event.ConsecutiveFails, event.Err)
				eventType := domain.TrajectoryEventSystemHealthCheckFailed
				if event.Fatal {
					eventType = domain.TrajectoryEventSystemSandboxDeath
					summary = fmt.Sprintf("%s sandbox declared dead during %s phase after %d consecutive failures: %v", s.runtimeName, phase, event.ConsecutiveFails, event.Err)
				}
				metadata := domain.KeyValue{
					"phase":             phase,
					"consecutive_fails": fmt.Sprintf("%d", event.ConsecutiveFails),
					"fatal":             fmt.Sprintf("%t", event.Fatal),
				}
				if reason := sandbox.ClassifyFatalReason(event.Err); reason != "" {
					metadata["fatal_reason"] = reason
				}
				metadata["error"] = event.Err.Error()
				_, _ = s.trajectory.append(domain.TrajectoryStep{
					Type:     eventType,
					Actor:    "rollout",
					Summary:  summary,
					Metadata: metadata,
				})
			},
		})
		var agentCancel context.CancelFunc
		phaseCtx, agentCancel = context.WithCancel(ctx)
		monitorDone := make(chan struct{})
		monitor.Start(ctx)
		go func() {
			select {
			case <-monitor.Dead():
				agentCancel()
			case <-monitorDone:
			}
		}()
		cleanup = func() {
			close(monitorDone)
			monitor.Stop()
			agentCancel()
		}
	}

	if timeoutSec > 0 {
		var agentTimeout context.CancelFunc
		phaseCtx, agentTimeout = context.WithTimeout(phaseCtx, time.Duration(timeoutSec)*time.Second)
		prevCleanup := cleanup
		cleanup = func() {
			agentTimeout()
			prevCleanup()
		}
	}

	return phaseCtx, monitor, cleanup
}

func (s *executionSession) agentTimeoutSec() int {
	if s.task.Timeouts == nil {
		return 0
	}
	return s.task.Timeouts.AgentSec
}

func (s *executionSession) verifierTimeoutSec() int {
	if s.task.Timeouts == nil {
		return 0
	}
	return s.task.Timeouts.VerifierSec
}

func (s *executionSession) finalizeWithoutVerifier() error {
	collectingStart := time.Now()
	s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusStarted, collectingStart, nil)
	if s.episode.FinishedAt == nil {
		err := fmt.Errorf("cannot finalize terminal episode without finished_at")
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	if strings.TrimSpace(s.paths.AgentJSONPath) == "" {
		err := fmt.Errorf("cannot finalize terminal episode without agent result path")
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	if strings.TrimSpace(s.paths.RewardJSONPath) == "" {
		err := fmt.Errorf("cannot finalize terminal episode without reward path")
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	finalizeEpisodeTiming(&s.episode, &s.timer)
	stampCompleted(&s.episode, s.request.Now)
	var err error
	s.episode, err = writeArtifactManifest(s.store, s.paths, s.episode, s.request.Now)
	if err != nil {
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, s.episode); err != nil {
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusCompleted, collectingStart, nil)
	return nil
}

func (s *executionSession) runVerifierPhase(ctx context.Context, instance sandbox.Instance) error {
	verifierCtx, monitor, cleanup := s.buildPhaseContext(ctx, instance, "verifier", s.verifierTimeoutSec())
	defer cleanup()

	result, err := runVerifierFlow(verifierCtx, verifierFlowRequest{
		store:      s.store,
		paths:      s.paths,
		task:       s.task,
		episode:    s.episode,
		sandbox:    instance,
		trajectory: s.trajectory,
		now:        s.request.Now,
	})
	s.episode = result.Episode
	if state, stateErr := instance.State(); stateErr == nil {
		s.episode.SandboxState = sandboxRuntimeState(state)
	}
	s.timer.verifierExecMS = result.DurationMS
	finalizeEpisodeTiming(&s.episode, &s.timer)
	if err != nil {
		if isSandboxDeathErr(monitor, err) || sandbox.IsFatalSandboxError(err) {
			cause := err
			if monitor != nil && monitor.Err() != nil {
				cause = monitor.Err()
			}
			return s.failInfrastructure(cause, failureOptions{Source: "verifier"})
		}
		if isTimeoutErr(err) {
			return s.failTimeout(failureOptions{Source: "verifier"})
		}
		return err
	}
	stampCompleted(&s.episode, s.request.Now)
	collectingStart := time.Now()
	s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusStarted, collectingStart, nil)
	s.episode, err = writeArtifactManifest(s.store, s.paths, s.episode, s.request.Now)
	if err != nil {
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	if err := s.store.WriteEpisode(s.paths.EpisodeJSONPath, s.episode); err != nil {
		s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusFailed, collectingStart, err)
		return err
	}
	s.emitPhase(domain.RolloutPhaseCollecting, domain.PhaseStatusCompleted, collectingStart, nil)
	return nil
}
