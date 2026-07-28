package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type EpisodePlan struct {
	Task    domain.TaskInstance
	Episode domain.Episode
}

type EpisodeExecution struct {
	Plan   EpisodePlan
	Layout localstore.EpisodeLayout
}

func newEpisodePlans(rolloutRun domain.RolloutRun, tasks []domain.TaskInstance) []EpisodePlan {
	attempts := rolloutRun.AttemptsPerTask
	if attempts < 1 {
		attempts = 1
	}
	plans := make([]EpisodePlan, 0, len(tasks)*attempts)
	for _, task := range tasks {
		for attemptIndex := 1; attemptIndex <= attempts; attemptIndex++ {
			plans = append(plans, EpisodePlan{
				Task:    task,
				Episode: newEpisode(rolloutRun, task, attemptIndex),
			})
		}
	}
	return plans
}

func createEpisodeLayouts(store localstore.Store, runLayout localstore.RunLayout, plans []EpisodePlan) ([]EpisodeExecution, error) {
	executions := make([]EpisodeExecution, 0, len(plans))
	for _, plan := range plans {
		layout, err := store.CreateEpisodeLayout(runLayout, plan.Task, plan.Episode)
		if err != nil {
			return nil, err
		}
		executions = append(executions, EpisodeExecution{Plan: plan, Layout: layout})
	}
	return executions, nil
}

func newRolloutPlan(rolloutRun domain.RolloutRun, selection domain.TaskSelection, plans []EpisodePlan, now time.Time) domain.RolloutPlan {
	episodes := make([]domain.PlannedEpisode, 0, len(plans))
	for index, plan := range plans {
		episodes = append(episodes, domain.PlannedEpisode{
			ID:           plan.Episode.ID,
			TaskID:       plan.Task.ID,
			AttemptIndex: plan.Episode.AttemptIndex,
			Order:        index + 1,
		})
	}
	return domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           rolloutRun.ID,
		CreatedAt:       now.UTC(),
		Input:           rolloutRun.Input,
		Selection:       selection,
		Concurrency:     rolloutRun.Concurrency,
		AttemptsPerTask: rolloutRun.AttemptsPerTask,
		Agent:           rolloutRun.Agent,
		Provider:        providerRequirement(rolloutRun.Agent),
		Model:           rolloutRun.Model,
		Sandbox:         rolloutRun.Sandbox,
		TaskIDs:         append([]string(nil), rolloutRun.TaskIDs...),
		Episodes:        episodes,
	}
}

func providerRequirement(agent domain.AgentSpec) *domain.ProviderRequirement {
	wireAPI, err := agentprofile.RequiredWireAPI(agentprofile.AgentType(agent.Name))
	if err != nil {
		return nil
	}
	return &domain.ProviderRequirement{WireAPI: string(wireAPI)}
}
