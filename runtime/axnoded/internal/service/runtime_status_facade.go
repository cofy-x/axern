package service

import (
	"context"
	"fmt"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/version"
)

func (h *sandboxService) Version(ctx context.Context, request *runtime.VersionRequest) (*runtime.VersionResponse, error) {
	resp := &runtime.VersionResponse{
		Version: version.Version,
	}
	if h.runscHandler == nil {
		return resp, nil
	}
	runsc, err := h.runscHandler.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("get runsc version: %w", err)
	}
	resp.Runsc = runsc
	return resp, nil
}
