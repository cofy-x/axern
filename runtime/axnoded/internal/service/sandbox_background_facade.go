package service

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type SandboxBackgroundService interface {
	BackgroundProcesses(string) (contract.BackgroundProcesses, error)
}

func (h *sandboxService) BackgroundProcesses(allocationID string) (contract.BackgroundProcesses, error) {
	if err := h.validateBoundAllocationExecution(allocationID); err != nil {
		return nil, err
	}
	sandbox, ok := h.runscHandler.(SandboxBackgroundService)
	if !ok {
		return nil, fmt.Errorf("background process runtime is unavailable: %w", errord.ErrFailedPrecondition)
	}
	return sandbox.BackgroundProcesses(allocationID)
}
