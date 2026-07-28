package rollout

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/application/resumepolicy"
	validateapp "github.com/cofy-x/axern/apps/axrun/internal/application/validate"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

// ResumeRunner returns the runner captured by an immutable rollout record.
// Operational adapters may use it to decide whether external context is
// required before asking Service to resume the run.
func ResumeRunner(runDir string) (string, error) {
	loaded, err := localstore.LoadRun(runDir)
	if err != nil {
		return "", err
	}
	runner := string(loaded.Layout.RolloutRun.Sandbox.Backend)
	if err := backend.ValidateName(runner); err != nil {
		return "", fmt.Errorf("load runner from rollout plan: %w", err)
	}
	return runner, nil
}

func (s Service) resume(params Params) (Result, error) {
	runID := ""
	if params.ResumeRunDir != "" {
		runID = filepath.Base(filepath.Clean(params.ResumeRunDir))
	}
	reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusStarted, nil)
	loaded, err := localstore.LoadRun(params.ResumeRunDir)
	if err != nil {
		reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	runID = loaded.Layout.RolloutRun.ID
	params.BackendName = string(loaded.Layout.RolloutRun.Sandbox.Backend)
	if err := backend.ValidateName(params.BackendName); err != nil {
		err = fmt.Errorf("load runner from rollout plan: %w", err)
		reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	store := localstore.New(filepath.Dir(loaded.Layout.RunDir))
	if err := refreshRunEnvelopeForResume(store, &loaded, s.Now); err != nil {
		reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusCompleted, nil)
	params.Agent = loaded.Layout.RolloutRun.Agent.Name
	if loaded.Layout.RolloutRun.Agent.Runtime != nil {
		params.AgentImage = loaded.Layout.RolloutRun.Agent.Runtime.Image
	}
	runnable := resumableExecutions(loaded.Episodes)
	if len(runnable) == 0 {
		if _, err := validateapp.Run(validateapp.Params{RunDir: loaded.Layout.RunDir}); err != nil {
			reportRunPhase(params, runID, domain.RolloutPhasePlanning, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
	}
	if len(runnable) > 0 {
		reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusStarted, nil)
		adapter, err := s.newBackend(params)
		if err != nil {
			reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		if providerProfilePreflight, ok := adapter.(backend.ProviderProfilePreflight); ok {
			if err := providerProfilePreflight.PreflightProviderProfile(loaded.Layout.RolloutRun.Agent); err != nil {
				reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
				return Result{}, err
			}
		}
		if err := s.validateRunAgentForBackend(loaded.Layout.RolloutRun, params.BackendName); err != nil {
			reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		if providerPreflight, ok := adapter.(backend.ProviderPreflight); ok {
			if err := providerPreflight.PreflightProvider(runContext(params), loaded.Layout.RolloutRun.Agent, loaded.Layout.RolloutRun.Model); err != nil {
				reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
				return Result{}, err
			}
		}
		if err := adapter.Preflight(); err != nil {
			reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		if taskPreflight, ok := adapter.(backend.TaskPreflight); ok {
			if err := taskPreflight.PreflightTasks(tasksFromExecutions(runnable)); err != nil {
				reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
				return Result{}, err
			}
		}
		reportRunPhase(params, runID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusCompleted, nil)
		run := loaded.Layout.RolloutRun
		run.Status = domain.RunStatusRunning
		run.UpdatedAt = timePtr(currentTime(s.Now))
		run.Summary = summaryPtr(summarizeRun(len(run.TaskIDs), loaded.Episodes))
		loaded.Layout.RolloutRun = run
		if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
			return Result{}, err
		}
		if err := resetSidecarsForResume(store, runnable); err != nil {
			return Result{}, err
		}
		if err := appendResumeSteps(store, runnable, s.Now); err != nil {
			return Result{}, err
		}
		executionResult, err := executeEpisodes(adapter, store, runnable, params.Concurrency, params.PhaseReporter)
		allLayouts := mergeExecutedLayouts(loaded.Episodes, executionResult.Layouts)
		run.UpdatedAt = timePtr(currentTime(s.Now))
		run.Summary = summaryPtr(summarizeRun(len(run.TaskIDs), allLayouts))
		if err != nil {
			run.Status = domain.RunStatusFailed
			run.Summary.InfraFailures = executionResult.InfraFailures
			_ = store.WriteRolloutRun(loaded.Layout.RunJSONPath, run)
			return Result{}, err
		}
		run.Status = runStatusForExecutions(allLayouts)
		loaded.Layout.RolloutRun = run
		if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
			return Result{}, err
		}
		reportRunPhase(params, runID, domain.RolloutPhaseValidating, domain.PhaseStatusStarted, nil)
		if _, err := validateapp.Run(validateapp.Params{RunDir: loaded.Layout.RunDir}); err != nil {
			run.Status = domain.RunStatusFailed
			run.UpdatedAt = timePtr(currentTime(s.Now))
			run.Summary.InfraFailures = 1
			_ = store.WriteRolloutRun(loaded.Layout.RunJSONPath, run)
			reportRunPhase(params, runID, domain.RolloutPhaseValidating, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		reportRunPhase(params, runID, domain.RolloutPhaseValidating, domain.PhaseStatusCompleted, nil)
		loaded.Layout.RolloutRun = run
		return resumeResult(loaded.Layout, allLayouts), nil
	}
	run := loaded.Layout.RolloutRun
	run.UpdatedAt = timePtr(currentTime(s.Now))
	run.Summary = summaryPtr(summarizeRun(len(run.TaskIDs), loaded.Episodes))
	run.Status = runStatusForExecutions(loaded.Episodes)
	loaded.Layout.RolloutRun = run
	if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
		return Result{}, err
	}
	return resumeResult(loaded.Layout, loaded.Episodes), nil
}

func resumeResult(runLayout localstore.RunLayout, layouts []localstore.EpisodeLayout) Result {
	result := buildResult(runLayout, layouts)
	result.Resumed = true
	return result
}

func refreshRunEnvelopeForResume(store localstore.Store, loaded *localstore.LoadedRun, nowFn func() time.Time) error {
	run := loaded.Layout.RolloutRun
	summary := summarizeRun(len(run.TaskIDs), loaded.Episodes)
	if run.Summary != nil && run.Summary.InfraFailures > 0 {
		summary.InfraFailures = run.Summary.InfraFailures
	}
	run.Summary = summaryPtr(summary)
	hasResumable := false
	for _, layout := range loaded.Episodes {
		if resumepolicy.Decide(layout).Action == resumepolicy.ActionExecute {
			hasResumable = true
			break
		}
	}
	switch {
	case hasResumable:
		run.Status = domain.RunStatusRunning
	case summary.InfraFailures > 0:
		run.Status = domain.RunStatusFailed
	default:
		run.Status = runStatusForExecutions(loaded.Episodes)
	}
	run.UpdatedAt = timePtr(currentTime(nowFn))
	loaded.Layout.RolloutRun = run
	if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
		return err
	}
	return nil
}

func resumableExecutions(layouts []localstore.EpisodeLayout) []EpisodeExecution {
	executions := []EpisodeExecution{}
	for _, layout := range layouts {
		if resumepolicy.Decide(layout).Action != resumepolicy.ActionExecute {
			continue
		}
		layout.Episode = resetEpisodeForResume(layout.Episode)
		executions = append(executions, EpisodeExecution{
			Plan: EpisodePlan{
				Task:    layout.TaskInstance,
				Episode: layout.Episode,
			},
			Layout: layout,
		})
	}
	return executions
}

func resetEpisodeForResume(episode domain.Episode) domain.Episode {
	episode.Status = domain.EpisodeStatusPending
	episode.StartedAt = nil
	episode.FinishedAt = nil
	episode.CompletedAt = nil
	episode.DurationMS = 0
	episode.FailureClass = ""
	episode.SandboxState = nil
	episode.Timing = nil
	episode.Usage = nil
	episode.Cost = nil
	episode.ArtifactManifestPath = ""
	episode.Artifacts = nil
	return episode
}

func appendResumeSteps(store localstore.Store, executions []EpisodeExecution, nowFn func() time.Time) error {
	for _, execution := range executions {
		count, err := store.CountTrajectorySteps(execution.Layout.TrajectoryPath)
		if err != nil {
			return fmt.Errorf("count trajectory steps for %s: %w", execution.Layout.Episode.ID, err)
		}
		step := domain.TrajectoryStep{
			EventID:   fmt.Sprintf("step-%06d", count+1),
			Index:     count + 1,
			Timestamp: currentTime(nowFn),
			Type:      domain.TrajectoryEventSystemResumeStarted,
			Actor:     "rollout",
			Summary:   "episode resume started",
		}
		if err := store.AppendTrajectoryStep(execution.Layout.TrajectoryPath, step); err != nil {
			return fmt.Errorf("append resume step for %s: %w", execution.Layout.Episode.ID, err)
		}
	}
	return nil
}

func tasksFromExecutions(executions []EpisodeExecution) []domain.TaskInstance {
	tasks := make([]domain.TaskInstance, 0, len(executions))
	for _, execution := range executions {
		tasks = append(tasks, execution.Layout.TaskInstance)
	}
	return tasks
}

func resetSidecarsForResume(store localstore.Store, executions []EpisodeExecution) error {
	for _, execution := range executions {
		verifierType := execution.Layout.TaskInstance.Verifier.Type
		if err := store.ResetEpisodeSidecarsForResume(execution.Layout, verifierType); err != nil {
			return fmt.Errorf("reset sidecars for episode %s: %w", execution.Layout.Episode.ID, err)
		}
	}
	return nil
}

func mergeExecutedLayouts(existing []localstore.EpisodeLayout, executed []localstore.EpisodeLayout) []localstore.EpisodeLayout {
	byID := make(map[string]localstore.EpisodeLayout, len(executed))
	for _, layout := range executed {
		byID[layout.Episode.ID] = layout
	}
	merged := make([]localstore.EpisodeLayout, 0, len(existing))
	for _, layout := range existing {
		if executedLayout, ok := byID[layout.Episode.ID]; ok {
			merged = append(merged, executedLayout)
			continue
		}
		merged = append(merged, layout)
	}
	return merged
}
