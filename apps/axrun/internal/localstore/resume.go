package localstore

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type LoadedRun struct {
	Layout   RunLayout
	Plan     domain.RolloutPlan
	Episodes []EpisodeLayout
}

func LoadRun(runDir string) (LoadedRun, error) {
	runDir = strings.TrimSpace(runDir)
	if runDir == "" {
		return LoadedRun{}, fmt.Errorf("rollout run directory is required")
	}
	cleanRunDir := filepath.Clean(runDir)
	runPath := filepath.Join(cleanRunDir, "run.json")
	planPath := filepath.Join(cleanRunDir, "plan.json")
	run, err := readJSONFile[domain.RolloutRun](runPath)
	if err != nil {
		return LoadedRun{}, fmt.Errorf("read run.json: %w", err)
	}
	plan, err := readJSONFile[domain.RolloutPlan](planPath)
	if err != nil {
		return LoadedRun{}, fmt.Errorf("read plan.json: %w", err)
	}
	layout := RunLayout{
		RunDir:       cleanRunDir,
		RunJSONPath:  runPath,
		PlanJSONPath: planPath,
		InputsDir:    filepath.Join(cleanRunDir, "inputs"),
		TasksDir:     filepath.Join(cleanRunDir, "tasks"),
		EpisodesDir:  filepath.Join(cleanRunDir, "episodes"),
		RolloutRun:   run,
	}
	episodes := make([]EpisodeLayout, 0, len(plan.Episodes))
	for _, planned := range plan.Episodes {
		episodeLayout, err := loadEpisodeLayout(layout, planned)
		if err != nil {
			return LoadedRun{}, err
		}
		episodes = append(episodes, episodeLayout)
	}
	return LoadedRun{Layout: layout, Plan: plan, Episodes: episodes}, nil
}

func loadEpisodeLayout(run RunLayout, planned domain.PlannedEpisode) (EpisodeLayout, error) {
	if err := contract.ValidatePathSegment("planned task id", planned.TaskID); err != nil {
		return EpisodeLayout{}, err
	}
	if err := contract.ValidatePathSegment("planned episode id", planned.ID); err != nil {
		return EpisodeLayout{}, err
	}
	taskDir := filepath.Join(run.TasksDir, planned.TaskID)
	taskJSONPath := filepath.Join(taskDir, "task.json")
	task, err := readJSONFile[domain.TaskInstance](taskJSONPath)
	if err != nil {
		return EpisodeLayout{}, fmt.Errorf("read task %q: %w", planned.TaskID, err)
	}
	episodeDir := filepath.Join(run.EpisodesDir, planned.ID)
	episodeJSONPath := filepath.Join(episodeDir, "episode.json")
	episode, err := readJSONFile[domain.Episode](episodeJSONPath)
	if err != nil {
		return EpisodeLayout{}, fmt.Errorf("read episode %q: %w", planned.ID, err)
	}
	return EpisodeLayout{
		TaskDir:          taskDir,
		TaskJSONPath:     taskJSONPath,
		EpisodeDir:       episodeDir,
		EpisodeJSONPath:  episodeJSONPath,
		TrajectoryPath:   filepath.Join(episodeDir, "trajectory.jsonl"),
		AgentJSONPath:    filepath.Join(episodeDir, "agent.json"),
		VerifierJSONPath: filepath.Join(episodeDir, "verifier.json"),
		RewardJSONPath:   filepath.Join(episodeDir, "reward.json"),
		ArtifactDir:      filepath.Join(episodeDir, "artifacts"),
		TaskInstance:     task,
		Episode:          episode,
	}, nil
}
