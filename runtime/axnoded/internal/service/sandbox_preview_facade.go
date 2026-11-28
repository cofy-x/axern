package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type SandboxHTTPPreviewService interface {
	PreviewHTTP(context.Context, string, wire.HTTPPreviewRequest) (wire.HTTPPreviewResponse, error)
}

func (h *sandboxService) PreviewHTTP(ctx context.Context, allocationID string, request wire.HTTPPreviewRequest) (wire.HTTPPreviewResponse, error) {
	if err := h.validateHTTPPreviewAllocation(allocationID); err != nil {
		return wire.HTTPPreviewResponse{}, err
	}
	executor, ok := h.runscHandler.(SandboxHTTPPreviewService)
	if !ok {
		return wire.HTTPPreviewResponse{}, fmt.Errorf("HTTP preview runtime is unavailable: %w", errord.ErrFailedPrecondition)
	}
	response, err := executor.PreviewHTTP(ctx, allocationID, request)
	if err != nil {
		return wire.HTTPPreviewResponse{}, err
	}
	if err := h.validateHTTPPreviewAllocation(allocationID); err != nil {
		return wire.HTTPPreviewResponse{}, err
	}
	return response, nil
}

func (h *sandboxService) validateHTTPPreviewAllocation(allocationID string) error {
	err := h.validateBoundAllocationExecution(allocationID)
	if errors.Is(err, errord.ErrFailedPrecondition) || errors.Is(err, errord.ErrNotFound) {
		return fmt.Errorf("allocation preview is no longer available: %w", errord.ErrNotFound)
	}
	return err
}
