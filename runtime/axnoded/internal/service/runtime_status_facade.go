package service

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/version"
)

type RuntimeStatus struct {
	Name         string
	Binary       string
	Loaded       bool
	Capabilities contract.RuntimeCapabilities
	Requirements contract.RuntimeRequirements
}

func (h *sandboxService) RuntimeStatuses() []RuntimeStatus {
	if h.runscHandler == nil {
		return nil
	}
	return []RuntimeStatus{{
		Name:         config.RuntimeNameRunsc,
		Binary:       h.config.RuntimeConfig.Runsc.Binary,
		Loaded:       true,
		Capabilities: h.runscHandler.Capabilities(),
		Requirements: h.runscHandler.Requirements(),
	}}
}

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
