package localstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// RunLayout describes the local filesystem layout created for one rollout run
// plus the top-level records owned by that layout.
type RunLayout struct {
	RunDir       string
	RunJSONPath  string
	PlanJSONPath string
	InputsDir    string
	TasksDir     string
	EpisodesDir  string
	RolloutRun   domain.RolloutRun
}

func (s Store) CreateRunLayout(run domain.RolloutRun) (RunLayout, error) {
	if err := contract.ValidatePathSegment("rollout run id", run.ID); err != nil {
		return RunLayout{}, err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return RunLayout{}, fmt.Errorf("create rollout run root: %w", err)
	}
	runDir := filepath.Join(s.root, run.ID)
	if err := os.Mkdir(runDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return RunLayout{}, fmt.Errorf("rollout run %q already exists at %s", run.ID, runDir)
		}
		return RunLayout{}, fmt.Errorf("create rollout run directory: %w", err)
	}
	inputsDir := filepath.Join(runDir, "inputs")
	tasksDir := filepath.Join(runDir, "tasks")
	episodesDir := filepath.Join(runDir, "episodes")
	if err := os.Mkdir(inputsDir, 0o755); err != nil {
		return RunLayout{}, fmt.Errorf("create inputs directory: %w", err)
	}
	if err := os.Mkdir(tasksDir, 0o755); err != nil {
		return RunLayout{}, fmt.Errorf("create tasks directory: %w", err)
	}
	if err := os.Mkdir(episodesDir, 0o755); err != nil {
		return RunLayout{}, fmt.Errorf("create episodes directory: %w", err)
	}
	run.OutputPath = "."
	runJSONPath := filepath.Join(runDir, "run.json")
	planJSONPath := filepath.Join(runDir, "plan.json")
	if err := writeJSON(runJSONPath, run); err != nil {
		return RunLayout{}, fmt.Errorf("write run.json: %w", err)
	}
	return RunLayout{
		RunDir:       runDir,
		RunJSONPath:  runJSONPath,
		PlanJSONPath: planJSONPath,
		InputsDir:    inputsDir,
		TasksDir:     tasksDir,
		EpisodesDir:  episodesDir,
		RolloutRun:   run,
	}, nil
}
