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
	ProviderRequirement *domain.ProviderRequirement
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
	prepared, err := prepareRolloutRun(runContext(params), params, now)
	if err != nil {
		return Result{}, err
	}
	rolloutRun := prepared.RolloutRun
	tasks := prepared.Tasks
	planSelection := prepared.PlanSelection
	adapter, err := s.newBackend(params)
	if err != nil {
		return Result{}, err
	}
	if params.Execute {
		if err := s.validateRunAgentForBackend(prepared.Agent, params.BackendName); err != nil {
			return Result{}, err
		}
		if providerPreflight, ok := adapter.(backend.ProviderPreflight); ok {
			if err := providerPreflight.PreflightProvider(runContext(params), prepared.Agent, prepared.Model); err != nil {
				return Result{}, err
			}
		}
		if err := adapter.Preflight(); err != nil {
			return Result{}, err
		}
		if taskPreflight, ok := adapter.(backend.TaskPreflight); ok {
			if err := taskPreflight.PreflightTasks(tasks); err != nil {
				return Result{}, err
			}
		}
	}
	store := localstore.New(params.Output)
	result, err := store.CreateRunLayout(rolloutRun)
	if err != nil {
		return Result{}, err
	}
	lock, err := localstore.AcquireRunLock(result.RunDir)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	rolloutRun = result.RolloutRun
	captured, err := store.CaptureInputs(runContext(params), result, prepared.Input, tasks, &prepared.TaskSet)
	if err != nil {
		return Result{}, err
	}
	prepared.Input = captured.Input
	tasks = captured.Tasks
	episodePlans := newEpisodePlans(rolloutRun.ID, prepared.Attempts, prepared.Agent, prepared.Model, tasks)
	executions, err := createEpisodeLayouts(store, result, episodePlans, prepared.Agent, prepared.Model)
	if err != nil {
		return Result{}, err
	}
	plan := newRolloutPlan(rolloutRun.ID, prepared.Input, planSelection, prepared.Concurrency, prepared.Attempts, prepared.Agent, params.ProviderRequirement, prepared.Model, prepared.Sandbox, tasks, episodePlans, now)
	if err := store.WriteRolloutPlan(result.PlanJSONPath, plan); err != nil {
		return Result{}, err
	}
	rolloutRun.PlanPath = "plan.json"
	rolloutRun.PlanDigest, err = domain.DigestRolloutPlan(plan)
	if err != nil {
		return Result{}, err
	}
	result.RolloutRun = rolloutRun
	result.RolloutPlan = plan
	layouts := make([]localstore.EpisodeLayout, 0, len(executions))
	for _, execution := range executions {
		layouts = append(layouts, execution.Layout)
	}
	result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
	if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
		return Result{}, err
	}
	if params.Execute {
		result.RolloutRun.Status = domain.RunStatusRunning
		result.RolloutRun.UpdatedAt = timePtr(now)
		result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
		if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
			return Result{}, err
		}
		executionResult, err := executeEpisodes(adapter, store, executions, params.Concurrency)
		layouts = executionResult.Layouts
		result.RolloutRun.UpdatedAt = timePtr(currentTime(s.Now))
		result.RolloutRun.Summary = summaryPtr(summarizeRun(len(tasks), layouts))
		if err != nil {
			result.RolloutRun.Status = domain.RunStatusFailed
			_ = store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun)
			return Result{}, err
		}
		result.RolloutRun.Status = runStatusForExecutions(layouts)
		if err := store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun); err != nil {
			return Result{}, err
		}
		if _, err := validateapp.Run(validateapp.Params{RunDir: result.RunDir}); err != nil {
			result.RolloutRun.Status = domain.RunStatusFailed
			result.RolloutRun.UpdatedAt = timePtr(currentTime(s.Now))
			_ = store.WriteRolloutRun(result.RunJSONPath, result.RolloutRun)
			return Result{}, err
		}
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
