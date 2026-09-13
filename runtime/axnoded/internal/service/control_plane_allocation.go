package service

import (
	"context"
	"fmt"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

// StartControlPlaneAllocation is the only path that creates the durable
// admission relation used by controld reporting. Ordinary node-local Start
// calls remain unbound and cannot enter control-plane inventory or lifecycle.
func (h *sandboxService) StartControlPlaneAllocation(ctx context.Context, nodeID string, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	if h == nil || request == nil {
		return nil, fmt.Errorf("control-plane allocation request is required")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("control-plane allocation node id is required")
	}
	return h.start(ctx, request, nodeID)
}

func (h *sandboxService) DeleteControlPlaneAllocation(ctx context.Context, nodeID string, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	if h == nil || request == nil {
		return nil, fmt.Errorf("control-plane allocation delete request is required")
	}
	return h.delete(ctx, request, nodeID)
}

func (h *sandboxService) HasControlPlaneAllocation(allocationID, nodeID string) bool {
	return h != nil && h.allocationController().ControlPlaneBindingMatches(allocationID, strings.TrimSpace(nodeID))
}
