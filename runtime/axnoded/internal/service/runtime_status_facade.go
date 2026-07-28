package service

import (
	"context"
	"fmt"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/cofy-x/axern/runtime/axnoded/version"
)

type RuntimeStatus struct {
	Name         string
	Binary       string
	Loaded       bool
	Capabilities contract.RuntimeCapabilities
	Requirements contract.RuntimeRequirements
}

func (h *sandboxService) checkRuntime(requestRuntime string) error {
	_, err := h.runtimeHandler(requestRuntime)
	return err
}

func (h *sandboxService) runtimeHandler(runtimeName string) (contract.RuntimeHandler, error) {
	if h.runtimeHandlers == nil {
		return nil, fmt.Errorf("runtime %s is not supported", runtimeName)
	}
	return handlerregistry.Lookup(runtimeName, h.runtimeHandlers)
}

func (h *sandboxService) RuntimeStatuses() []RuntimeStatus {
	if h.runtimeHandlers == nil {
		return nil
	}
	statuses := h.runtimeHandlers.Statuses()
	out := make([]RuntimeStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, RuntimeStatus{
			Name:         status.Name,
			Binary:       status.Binary,
			Loaded:       status.Loaded,
			Capabilities: status.Capabilities,
			Requirements: status.Requirements,
		})
	}
	return out
}

func (h *sandboxService) Version(ctx context.Context, request *runtime.VersionRequest) (*runtime.VersionResponse, error) {
	resp := &runtime.VersionResponse{
		Version:  version.Version,
		Runtimes: make([]*runtime.RuntimeVersion, 0),
	}
	if h.runtimeHandlers == nil {
		return resp, nil
	}
	versions, err := h.runtimeHandlers.Version(ctx)
	if err != nil {
		return nil, err
	}
	resp.Runtimes = append(resp.Runtimes, versions...)
	return resp, nil
}
