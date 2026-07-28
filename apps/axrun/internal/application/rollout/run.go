package rollout

import (
	"context"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	validateapp "github.com/cofy-x/axern/apps/axrun/internal/application/validate"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	axernbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/axern"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

type Params struct {
	Agent               string
	AgentImage          string
	AgentProfile        string
	AgentApprovalPolicy string
	AgentCommand        string
	AgentCWD            string
	AgentUser           string
	AgentTimeoutSec     int
	AgentMaxTurns       int
	AgentOutputFormat   string
	AgentAllowedTools   []string
	AgentIdleTimeoutSec int
	AgentPatchPath      string
	AgentPatchRequired  bool
	AgentEnv            []string
	Model               string
	RuntimeClass        string
	RunID               string
	TaskSetRef          string
	SelectedTaskIDs     []string
	TaskLimit           int
	ShardIndex          int
	ShardCount          int
	ResumeRunDir        string
	Execute             bool
	BackendName         string
	Concurrency         int
	Attempts            int
	Output              string
	AxernConfig         *axernbackend.Config
	PhaseReporter       domain.PhaseReporter
	Context             context.Context
}

type Service struct {
	Now            func() time.Time
	BackendFactory func(BackendRequest) (backend.Backend, error)
	AgentRegistry  *agent.Registry
}

func (s Service) Run(params Params) (Result, error) {
	normalized, err := NormalizeParams(params)
	if err != nil {
		return Result{}, err
	}
	if normalized.ResumeRunDir != "" {
		return s.resume(normalized)
	}
	return s.create(normalized)
}

func (s Service) create(params Params) (Result, error) {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	reportRunPhase(params, params.RunID, domain.RolloutPhasePlanning, domain.PhaseStatusStarted, nil)
	prepared, err := prepareRolloutRun(runContext(params), params, now)
	if err != nil {
		reportRunPhase(params, params.RunID, domain.RolloutPhasePlanning, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	rolloutRun := prepared.RolloutRun
	reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePlanning, domain.PhaseStatusCompleted, nil)
	tasks := prepared.Tasks
	planSelection := prepared.PlanSelection
	reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusStarted, nil)
	adapter, err := s.newBackend(params)
	if err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	if providerProfilePreflight, ok := adapter.(backend.ProviderProfilePreflight); ok {
		if err := providerProfilePreflight.PreflightProviderProfile(rolloutRun.Agent); err != nil {
			reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
	}
	if params.Execute {
		if err := s.validateRunAgentForBackend(rolloutRun, params.BackendName); err != nil {
			reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		if providerPreflight, ok := adapter.(backend.ProviderPreflight); ok {
			if err := providerPreflight.PreflightProvider(runContext(params), rolloutRun.Agent, rolloutRun.Model); err != nil {
				reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
				return Result{}, err
			}
		}
		if err := adapter.Preflight(); err != nil {
			reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		if taskPreflight, ok := adapter.(backend.TaskPreflight); ok {
			if err := taskPreflight.PreflightTasks(tasks); err != nil {
				reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
				return Result{}, err
			}
		}
	}
	store := localstore.New(params.Output)
	result, err := store.CreateRunLayout(rolloutRun)
	if err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	rolloutRun = result.RolloutRun
	captured, err := store.CaptureInputs(result, rolloutRun.Input, tasks, &prepared.TaskSet)
	if err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	rolloutRun.Input = captured.Input
	tasks = captured.Tasks
	rolloutRun.TaskIDs = taskIDs(tasks)
	result.RolloutRun = rolloutRun
	episodePlans := newEpisodePlans(rolloutRun, tasks)
	executions, err := createEpisodeLayouts(store, result, episodePlans)
	if err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	plan := newRolloutPlan(rolloutRun, planSelection, episodePlans, now)
	if err := store.WriteRolloutPlan(result.PlanJSONPath, plan); err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	layouts := make([]localstore.EpisodeLayout, 0, len(executions))
	for _, execution := range executions {
		layouts = append(layouts, execution.Layout)
	}
	result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
	if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusFailed, err)
		return Result{}, err
	}
	reportRunPhase(params, rolloutRun.ID, domain.RolloutPhasePreparingInputs, domain.PhaseStatusCompleted, nil)
	if params.Execute {
		result.RolloutRun.Status = domain.RunStatusRunning
		result.RolloutRun.UpdatedAt = timePtr(now)
		result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
		if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
			return Result{}, err
		}
		executionResult, err := executeEpisodes(adapter, store, executions, params.Concurrency, params.PhaseReporter)
		layouts = executionResult.Layouts
		result.RolloutRun.UpdatedAt = timePtr(currentTime(s.Now))
		result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
		if err != nil {
			result.RolloutRun.Status = domain.RunStatusFailed
			result.RolloutRun.Summary.InfraFailures = executionResult.InfraFailures
			_ = store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun)
			return Result{}, err
		}
		result.RolloutRun.Status = runStatusForExecutions(layouts)
		if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
			return Result{}, err
		}
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhaseValidating, domain.PhaseStatusStarted, nil)
		if _, err := validateapp.Run(validateapp.Params{RunDir: result.RunDir}); err != nil {
			result.RolloutRun.Status = domain.RunStatusFailed
			result.RolloutRun.UpdatedAt = timePtr(currentTime(s.Now))
			result.RolloutRun.Summary.InfraFailures = 1
			_ = store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun)
			reportRunPhase(params, rolloutRun.ID, domain.RolloutPhaseValidating, domain.PhaseStatusFailed, err)
			return Result{}, err
		}
		reportRunPhase(params, rolloutRun.ID, domain.RolloutPhaseValidating, domain.PhaseStatusCompleted, nil)
	}
	return buildResult(result, layouts), nil
}

func runContext(params Params) context.Context {
	if params.Context != nil {
		return params.Context
	}
	return context.Background()
}

func currentTime(clock func() time.Time) time.Time {
	if clock != nil {
		return clock().UTC()
	}
	return time.Now().UTC()
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func summaryPtr(value domain.RunSummary) *domain.RunSummary {
	return &value
}
