package rollout

import (
	"fmt"
	"sync"

	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
	rolloutengine "github.com/cofy-x/axern/apps/axrun/internal/rollout"
)

type executionResult struct {
	index             int
	episode           domain.Episode
	agentResult       domain.AgentResult
	infrastructureErr error
}

type executeEpisodesResult struct {
	Layouts []localstore.EpisodeLayout
}

func executeEpisodes(adapter backend.Backend, store localstore.Store, executions []EpisodeExecution, concurrency int) (executeEpisodesResult, error) {
	if len(executions) == 0 {
		return executeEpisodesResult{}, nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(executions) {
		concurrency = len(executions)
	}
	layouts := make([]localstore.EpisodeLayout, len(executions))
	for index, execution := range executions {
		layouts[index] = execution.Layout
	}
	jobs := make(chan int)
	results := make(chan executionResult, len(executions))
	var once sync.Once
	stop := make(chan struct{})
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				select {
				case <-stop:
					continue
				default:
				}
				layout := executions[index].Layout
				episode, err := adapter.Execute(backend.ExecuteRequest{
					Store:   store,
					Task:    layout.TaskInstance,
					Episode: layout.Episode,
					Agent:   executions[index].Agent,
					Model:   executions[index].Model,
					Paths:   rolloutPaths(layout),
				})
				agentResult, readErr := localstore.ReadAgentResult(layout.AgentJSONPath)
				if err == nil && readErr != nil {
					err = readErr
				}
				if err != nil {
					once.Do(func() { close(stop) })
				}
				results <- executionResult{index: index, episode: episode, agentResult: agentResult, infrastructureErr: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range executions {
			select {
			case <-stop:
				return
			case jobs <- index:
			}
		}
	}()
	workers.Wait()
	close(results)
	var firstErr error
	for result := range results {
		if result.infrastructureErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("execute episode %s: %w", executions[result.index].Layout.Episode.ID, result.infrastructureErr)
			}
		}
		if result.episode.ID != "" {
			layouts[result.index].Episode = result.episode
		}
		layouts[result.index].AgentResult = result.agentResult
	}
	return executeEpisodesResult{Layouts: layouts}, firstErr
}

func rolloutPaths(layout localstore.EpisodeLayout) rolloutengine.Paths {
	return rolloutengine.Paths{
		EpisodeJSONPath:  layout.EpisodeJSONPath,
		TrajectoryPath:   layout.TrajectoryPath,
		AgentJSONPath:    layout.AgentJSONPath,
		VerifierJSONPath: layout.VerifierJSONPath,
		RewardJSONPath:   layout.RewardJSONPath,
		ArtifactDir:      layout.ArtifactDir,
	}
}
