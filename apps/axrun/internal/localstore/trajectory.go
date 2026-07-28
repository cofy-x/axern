package localstore

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (s Store) AppendTrajectoryStep(path string, step domain.TrajectoryStep) error {
	if err := appendJSONLine(path, step); err != nil {
		return fmt.Errorf("append trajectory.jsonl: %w", err)
	}
	return nil
}

func (s Store) CountTrajectorySteps(path string) (int, error) {
	count, err := countJSONLines(path)
	if err != nil {
		return 0, fmt.Errorf("count trajectory.jsonl steps: %w", err)
	}
	return count, nil
}
