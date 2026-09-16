package exportdata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func loadEpisodes(runDir string) ([]domain.Episode, error) {
	entries, err := os.ReadDir(filepath.Join(runDir, "episodes"))
	if err != nil {
		return nil, fmt.Errorf("read episodes directory: %w", err)
	}
	var episodes []domain.Episode
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		episode, err := readJSONFile[domain.Episode](filepath.Join(runDir, "episodes", entry.Name(), "episode.json"))
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].TaskID == episodes[j].TaskID {
			return episodes[i].AttemptIndex < episodes[j].AttemptIndex
		}
		return episodes[i].TaskID < episodes[j].TaskID
	})
	return episodes, nil
}

func loadEpisodeBundle(runDir string, outputPath string, run domain.RolloutRun, plan domain.RolloutPlan, episode domain.Episode) (episodeBundle, error) {
	taskPath := filepath.Join(runDir, "tasks", episode.TaskID, "task.json")
	task, err := readJSONFile[domain.TaskInstance](taskPath)
	if err != nil {
		return episodeBundle{}, err
	}
	episodeDir := filepath.Join(runDir, "episodes", episode.ID)
	agent, err := readJSONFile[domain.AgentResult](filepath.Join(episodeDir, "agent.json"))
	if err != nil {
		return episodeBundle{}, err
	}
	verifier, err := readJSONFile[domain.VerifierResult](filepath.Join(episodeDir, "verifier.json"))
	if err != nil {
		return episodeBundle{}, err
	}
	reward, err := readJSONFile[domain.Reward](filepath.Join(episodeDir, "reward.json"))
	if err != nil {
		return episodeBundle{}, err
	}
	return episodeBundle{
		RunRoot:  runDir,
		Run:      run,
		Plan:     plan,
		Task:     task,
		Episode:  episode,
		Agent:    agent,
		Verifier: verifier,
		Reward:   reward,
		Refs:     buildRefs(runDir, outputPath, episode, taskPath, agent),
	}, nil
}
