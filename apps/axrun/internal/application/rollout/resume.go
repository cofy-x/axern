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

type ResumeDescriptor struct {
	Runner   string
	Profile  string
	Provider *domain.ProviderRequirement
}

// DescribeResume returns the immutable execution dependencies an operational
// adapter must resolve before asking Service to resume a run.
func DescribeResume(runDir string) (ResumeDescriptor, error) {
	loaded, err := localstore.LoadRun(runDir)
	if err != nil {
		return ResumeDescriptor{}, err
	}
	runner := string(loaded.Plan.Sandbox.Backend)
	if err := backend.ValidateName(runner); err != nil {
		return ResumeDescriptor{}, fmt.Errorf("load runner from rollout plan: %w", err)
	}
	return ResumeDescriptor{Runner: runner, Profile: loaded.Plan.Agent.Profile, Provider: loaded.Plan.Provider}, nil
}

func (s Service) resume(params Params) (Result, error) {
	lock, err := localstore.AcquireRunLock(params.ResumeRunDir)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	loaded, err := localstore.LoadRun(params.ResumeRunDir)
	if err != nil {
		return Result{}, err
	}
	if !providerRequirementsEqual(loaded.Plan.Provider, params.ProviderRequirement) {
		err = fmt.Errorf("agent profile behavior changed since planning; create a new rollout instead of resuming with different provider configuration")
		return Result{}, err
	}
	params.Concurrency = loaded.Plan.Concurrency
	if err := finalizeInterruptedEpisodes(localstore.New(filepath.Dir(loaded.Layout.RunDir)), &loaded, s.Now); err != nil {
		return Result{}, err
	}
	params.BackendName = string(loaded.Plan.Sandbox.Backend)
	if err := backend.ValidateName(params.BackendName); err != nil {
		err = fmt.Errorf("load runner from rollout plan: %w", err)
		return Result{}, err
	}
	store := localstore.New(filepath.Dir(loaded.Layout.RunDir))
	if err := refreshRunEnvelopeForResume(store, &loaded, s.Now); err != nil {
		return Result{}, err
	}
	params.Agent = loaded.Plan.Agent.Name
	if loaded.Plan.Agent.Runtime != nil {
		params.AgentImage = loaded.Plan.Agent.Runtime.Image
	}
	runnable := resumableExecutions(loaded.Episodes, loaded.Plan.Agent, loaded.Plan.Model)
	if len(runnable) == 0 {
		if _, err := validateapp.Run(validateapp.Params{RunDir: loaded.Layout.RunDir}); err != nil {
			return Result{}, err
		}
	}
	if len(runnable) > 0 {
		adapter, err := s.newBackend(params)
		if err != nil {
			return Result{}, err
		}
		if err := s.validateRunAgentForBackend(loaded.Plan.Agent, params.BackendName); err != nil {
			return Result{}, err
		}
		if providerPreflight, ok := adapter.(backend.ProviderPreflight); ok {
			if err := providerPreflight.PreflightProvider(runContext(params), loaded.Plan.Agent, loaded.Plan.Model); err != nil {
				return Result{}, err
			}
		}
		if err := adapter.Preflight(); err != nil {
			return Result{}, err
		}
		if taskPreflight, ok := adapter.(backend.TaskPreflight); ok {
			if err := taskPreflight.PreflightTasks(tasksFromExecutions(runnable)); err != nil {
				return Result{}, err
			}
		}
		run := loaded.Layout.RolloutRun
		run.Status = domain.RunStatusRunning
		run.UpdatedAt = timePtr(currentTime(s.Now))
		run.Summary = summaryPtr(summarizeRun(len(loaded.Plan.TaskIDs), loaded.Episodes))
		loaded.Layout.RolloutRun = run
		if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
			return Result{}, err
		}
		executionResult, err := executeEpisodes(adapter, store, runnable, params.Concurrency)
		allLayouts := mergeExecutedLayouts(loaded.Episodes, executionResult.Layouts)
		run.UpdatedAt = timePtr(currentTime(s.Now))
		run.Summary = summaryPtr(summarizeRun(len(loaded.Plan.TaskIDs), allLayouts))
		if err != nil {
			run.Status = domain.RunStatusFailed
			_ = store.WriteRolloutRun(loaded.Layout.RunJSONPath, run)
			return Result{}, err
		}
		run.Status = runStatusForExecutions(allLayouts)
		loaded.Layout.RolloutRun = run
		if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
			return Result{}, err
		}
		if _, err := validateapp.Run(validateapp.Params{RunDir: loaded.Layout.RunDir}); err != nil {
			run.Status = domain.RunStatusFailed
			run.UpdatedAt = timePtr(currentTime(s.Now))
			_ = store.WriteRolloutRun(loaded.Layout.RunJSONPath, run)
			return Result{}, err
		}
		loaded.Layout.RolloutRun = run
		return resumeResult(loaded.Layout, allLayouts), nil
	}
	run := loaded.Layout.RolloutRun
	run.UpdatedAt = timePtr(currentTime(s.Now))
	run.Summary = summaryPtr(summarizeRun(len(loaded.Plan.TaskIDs), loaded.Episodes))
	run.Status = runStatusForExecutions(loaded.Episodes)
	loaded.Layout.RolloutRun = run
	if err := store.WriteRolloutRun(loaded.Layout.RunJSONPath, run); err != nil {
		return Result{}, err
	}
	return resumeResult(loaded.Layout, loaded.Episodes), nil
}

func providerRequirementsEqual(expected, actual *domain.ProviderRequirement) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return *expected == *actual
}

func resumeResult(runLayout localstore.RunLayout, layouts []localstore.EpisodeLayout) Result {
	result := buildResult(runLayout, layouts)
	result.Resumed = true
	return result
}

func refreshRunEnvelopeForResume(store localstore.Store, loaded *localstore.LoadedRun, nowFn func() time.Time) error {
	run := loaded.Layout.RolloutRun
	summary := summarizeRun(len(loaded.Plan.TaskIDs), loaded.Episodes)
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

func resumableExecutions(layouts []localstore.EpisodeLayout, agent domain.AgentSpec, model domain.ModelSpec) []EpisodeExecution {
	executions := []EpisodeExecution{}
	for _, layout := range layouts {
		if resumepolicy.Decide(layout).Action != resumepolicy.ActionExecute {
			continue
		}
		executions = append(executions, EpisodeExecution{
			Plan: EpisodePlan{
				Task:    layout.TaskInstance,
				Episode: layout.Episode,
			},
			Layout: layout,
			Agent:  agent,
			Model:  model,
		})
	}
	return executions
}

func finalizeInterruptedEpisodes(store localstore.Store, loaded *localstore.LoadedRun, nowFn func() time.Time) error {
	for index := range loaded.Episodes {
		layout := &loaded.Episodes[index]
		if resumepolicy.Decide(*layout).Action != resumepolicy.ActionFinalizeInterrupted {
			continue
		}
		finishedAt := currentTime(nowFn)
		layout.Episode.Status = domain.EpisodeStatusFailed
		layout.Episode.FailureClass = domain.FailureClassInfrastructure
		layout.Episode.FinishedAt = &finishedAt
		layout.Episode.CompletedAt = &finishedAt
		if err := store.WriteEpisode(layout.EpisodeJSONPath, layout.Episode); err != nil {
			return fmt.Errorf("finalize interrupted episode %s: %w", layout.Episode.ID, err)
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
