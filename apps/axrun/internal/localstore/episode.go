package localstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// EpisodeLayout describes the local filesystem layout created for one task
// and episode pair plus the task and episode records as written to disk.
type EpisodeLayout struct {
	TaskDir          string
	TaskJSONPath     string
	EpisodeDir       string
	EpisodeJSONPath  string
	TrajectoryPath   string
	AgentJSONPath    string
	VerifierJSONPath string
	RewardJSONPath   string
	ArtifactDir      string
	TaskInstance     domain.TaskInstance
	Episode          domain.Episode
}

func (s Store) CreateEpisodeLayout(run RunLayout, task domain.TaskInstance, episode domain.Episode) (EpisodeLayout, error) {
	if err := contract.ValidatePathSegment("task id", task.ID); err != nil {
		return EpisodeLayout{}, err
	}
	if err := contract.ValidatePathSegment("episode id", episode.ID); err != nil {
		return EpisodeLayout{}, err
	}

	taskDir := filepath.Join(run.TasksDir, task.ID)
	taskJSONPath := filepath.Join(taskDir, "task.json")
	if err := ensureTaskRecord(taskDir, taskJSONPath, task); err != nil {
		return EpisodeLayout{}, err
	}

	episodeDir := filepath.Join(run.EpisodesDir, episode.ID)
	if err := os.Mkdir(episodeDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return EpisodeLayout{}, fmt.Errorf("episode %q already exists at %s", episode.ID, episodeDir)
		}
		return EpisodeLayout{}, fmt.Errorf("create episode directory: %w", err)
	}
	trajectoryPath := filepath.Join(episodeDir, "trajectory.jsonl")
	agentJSONPath := filepath.Join(episodeDir, "agent.json")
	verifierJSONPath := filepath.Join(episodeDir, "verifier.json")
	rewardJSONPath := filepath.Join(episodeDir, "reward.json")
	artifactDir := filepath.Join(episodeDir, "artifacts")
	episodeJSONPath := filepath.Join(episodeDir, "episode.json")

	episode.TrajectoryPath = runRelativePath(run.RunDir, trajectoryPath)
	episode.AgentResultPath = runRelativePath(run.RunDir, agentJSONPath)
	episode.VerifierResultPath = runRelativePath(run.RunDir, verifierJSONPath)
	episode.RewardPath = runRelativePath(run.RunDir, rewardJSONPath)
	episode.ArtifactDir = runRelativePath(run.RunDir, artifactDir)

	if err := writeJSON(episodeJSONPath, episode); err != nil {
		return EpisodeLayout{}, fmt.Errorf("write episode.json: %w", err)
	}
	if err := os.WriteFile(trajectoryPath, nil, 0o644); err != nil {
		return EpisodeLayout{}, fmt.Errorf("write trajectory.jsonl: %w", err)
	}
	if err := writeJSON(agentJSONPath, domain.AgentResult{Status: domain.AgentStatusPending}); err != nil {
		return EpisodeLayout{}, fmt.Errorf("write agent.json: %w", err)
	}
	if err := writeJSON(verifierJSONPath, domain.VerifierResult{
		Status: domain.EpisodeStatusPending,
		Type:   task.Verifier.Type,
	}); err != nil {
		return EpisodeLayout{}, fmt.Errorf("write verifier.json: %w", err)
	}
	if err := writeJSON(rewardJSONPath, domain.Reward{Status: domain.RewardStatusPending}); err != nil {
		return EpisodeLayout{}, fmt.Errorf("write reward.json: %w", err)
	}
	if err := os.Mkdir(artifactDir, 0o755); err != nil {
		return EpisodeLayout{}, fmt.Errorf("create artifacts directory: %w", err)
	}

	return EpisodeLayout{
		TaskDir:          taskDir,
		TaskJSONPath:     taskJSONPath,
		EpisodeDir:       episodeDir,
		EpisodeJSONPath:  episodeJSONPath,
		TrajectoryPath:   trajectoryPath,
		AgentJSONPath:    agentJSONPath,
		VerifierJSONPath: verifierJSONPath,
		RewardJSONPath:   rewardJSONPath,
		ArtifactDir:      artifactDir,
		TaskInstance:     task,
		Episode:          episode,
	}, nil
}

func ensureTaskRecord(taskDir string, taskJSONPath string, task domain.TaskInstance) error {
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fmt.Errorf("create task directory: %w", err)
	}
	if _, err := os.Stat(taskJSONPath); err == nil {
		return validateExistingTaskRecord(taskJSONPath, task)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat task.json: %w", err)
	}
	if err := writeJSON(taskJSONPath, task); err != nil {
		return fmt.Errorf("write task.json: %w", err)
	}
	return nil
}

func validateExistingTaskRecord(path string, task domain.TaskInstance) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read existing task.json: %w", err)
	}
	var existing domain.TaskInstance
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("decode existing task.json: %w", err)
	}
	existingJSON, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("encode existing task.json: %w", err)
	}
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("encode task %q: %w", task.ID, err)
	}
	if string(existingJSON) != string(taskJSON) {
		return fmt.Errorf("existing task record at %s does not match task %q", path, task.ID)
	}
	return nil
}
