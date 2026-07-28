package rollout

import (
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

type Result struct {
	Resumed          bool
	RunID            string
	TaskID           string
	EpisodeID        string
	RunDir           string
	RunJSONPath      string
	PlanJSONPath     string
	TaskJSONPath     string
	EpisodeJSONPath  string
	TrajectoryPath   string
	AgentJSONPath    string
	VerifierJSONPath string
	RewardJSONPath   string
	ArtifactDir      string
	Status           domain.RunStatus
	EpisodeStatus    domain.EpisodeStatus
	TaskCount        int
	EpisodeCount     int
	AttemptsPerTask  int
	Summary          domain.RunSummary
	Episodes         []EpisodeResult
}

type EpisodeResult struct {
	TaskID           string
	EpisodeID        string
	TaskJSONPath     string
	EpisodeJSONPath  string
	TrajectoryPath   string
	AgentJSONPath    string
	VerifierJSONPath string
	RewardJSONPath   string
	ArtifactDir      string
	Status           domain.EpisodeStatus
}

func buildResult(runLayout localstore.RunLayout, layouts []localstore.EpisodeLayout) Result {
	taskCount := len(runLayout.RolloutRun.TaskIDs)
	if taskCount == 0 {
		taskCount = len(layouts)
	}
	result := Result{
		RunID:           runLayout.RolloutRun.ID,
		RunDir:          runLayout.RunDir,
		RunJSONPath:     runLayout.RunJSONPath,
		PlanJSONPath:    runLayout.PlanJSONPath,
		Status:          runLayout.RolloutRun.Status,
		TaskCount:       taskCount,
		EpisodeCount:    len(layouts),
		AttemptsPerTask: runLayout.RolloutRun.AttemptsPerTask,
		Summary:         summarizeRun(taskCount, layouts),
		Episodes:        make([]EpisodeResult, 0, len(layouts)),
	}
	for _, layout := range layouts {
		result.Episodes = append(result.Episodes, EpisodeResult{
			TaskID:           layout.TaskInstance.ID,
			EpisodeID:        layout.Episode.ID,
			TaskJSONPath:     layout.TaskJSONPath,
			EpisodeJSONPath:  layout.EpisodeJSONPath,
			TrajectoryPath:   layout.TrajectoryPath,
			AgentJSONPath:    layout.AgentJSONPath,
			VerifierJSONPath: layout.VerifierJSONPath,
			RewardJSONPath:   layout.RewardJSONPath,
			ArtifactDir:      layout.ArtifactDir,
			Status:           layout.Episode.Status,
		})
	}
	if len(result.Episodes) > 0 {
		result.applyPrimaryEpisode(result.Episodes[0])
	}
	return result
}

func (r *Result) applyPrimaryEpisode(episode EpisodeResult) {
	r.TaskID = episode.TaskID
	r.EpisodeID = episode.EpisodeID
	r.TaskJSONPath = episode.TaskJSONPath
	r.EpisodeJSONPath = episode.EpisodeJSONPath
	r.TrajectoryPath = episode.TrajectoryPath
	r.AgentJSONPath = episode.AgentJSONPath
	r.VerifierJSONPath = episode.VerifierJSONPath
	r.RewardJSONPath = episode.RewardJSONPath
	r.ArtifactDir = episode.ArtifactDir
	r.EpisodeStatus = episode.Status
}
