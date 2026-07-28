package backend

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/rollout"
)

type Name string

const (
	NameLocal Name = "local"
	NameAxern Name = "axern"
)

func ValidateName(name string) error {
	switch Name(name) {
	case NameLocal, NameAxern:
		return nil
	default:
		return fmt.Errorf("backend must be one of: local, axern")
	}
}

type ExecuteRequest struct {
	Store         rollout.Store
	Task          domain.TaskInstance
	Episode       domain.Episode
	Paths         rollout.Paths
	PhaseReporter domain.PhaseReporter
}

// Backend is the thin CLI-selected adapter around the shared rollout execution
// contract. External task input parsing, agent behavior, verifier logic, reward
// normalization, and trajectory schema belong outside backend packages.
type Backend interface {
	Preflight() error
	Execute(request ExecuteRequest) (domain.Episode, error)
}

type TaskPreflight interface {
	PreflightTasks(tasks []domain.TaskInstance) error
}
