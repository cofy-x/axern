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
	infrastructureErr error
}

type executeEpisodesResult struct {
	Layouts       []localstore.EpisodeLayout
	InfraFailures int
}

func executeEpisodes(adapter backend.Backend, store localstore.Store, executions []EpisodeExecution, concurrency int, reporter domain.PhaseReporter) (executeEpisodesResult, error) {
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
					Store:         store,
					Task:          layout.TaskInstance,
					Episode:       layout.Episode,
					Paths:         rolloutPaths(layout),
					PhaseReporter: reporter,
				})
				if err != nil {
					once.Do(func() { close(stop) })
				}
				results <- executionResult{index: index, episode: episode, infrastructureErr: err}
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
	infraFailures := 0
	for result := range results {
		if result.infrastructureErr != nil {
			infraFailures++
			if firstErr == nil {
				firstErr = fmt.Errorf("execute episode %s: %w", executions[result.index].Layout.Episode.ID, result.infrastructureErr)
			}
		}
		if result.episode.ID != "" {
			layouts[result.index].Episode = result.episode
		}
	}
	return executeEpisodesResult{Layouts: layouts, InfraFailures: infraFailures}, firstErr
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
