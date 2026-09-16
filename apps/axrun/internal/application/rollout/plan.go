package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

type EpisodePlan struct {
	Task    domain.TaskInstance
	Episode domain.Episode
}

type EpisodeExecution struct {
	Plan   EpisodePlan
	Layout localstore.EpisodeLayout
	Agent  domain.AgentSpec
	Model  domain.ModelSpec
}

func newEpisodePlans(runID string, attempts int, agent domain.AgentSpec, model domain.ModelSpec, tasks []domain.TaskInstance) []EpisodePlan {
	if attempts < 1 {
		attempts = 1
	}
	plans := make([]EpisodePlan, 0, len(tasks)*attempts)
	for _, task := range tasks {
		for attemptIndex := 1; attemptIndex <= attempts; attemptIndex++ {
			plans = append(plans, EpisodePlan{
				Task:    task,
				Episode: newEpisode(runID, task, attemptIndex),
			})
		}
	}
	return plans
}

func createEpisodeLayouts(store localstore.Store, runLayout localstore.RunLayout, plans []EpisodePlan, agent domain.AgentSpec, model domain.ModelSpec) ([]EpisodeExecution, error) {
	executions := make([]EpisodeExecution, 0, len(plans))
	for _, plan := range plans {
		layout, err := store.CreateEpisodeLayout(runLayout, plan.Task, plan.Episode)
		if err != nil {
			return nil, err
		}
		executions = append(executions, EpisodeExecution{Plan: plan, Layout: layout, Agent: agent, Model: model})
	}
	return executions, nil
}

func newRolloutPlan(runID string, input *domain.InputSpec, selection domain.TaskSelection, concurrency, attempts int, agent domain.AgentSpec, provider *domain.ProviderRequirement, model domain.ModelSpec, sandbox domain.SandboxSpec, tasks []domain.TaskInstance, plans []EpisodePlan, now time.Time) domain.RolloutPlan {
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
		RunID:           runID,
		CreatedAt:       now.UTC(),
		Input:           input,
		Selection:       selection,
		Concurrency:     concurrency,
		AttemptsPerTask: attempts,
		Agent:           agent,
		Provider:        provider,
		Model:           model,
		Sandbox:         sandbox,
		TaskIDs:         taskIDs(tasks),
		Episodes:        episodes,
	}
}
