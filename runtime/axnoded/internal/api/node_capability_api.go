package api

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *nodeSandboxServer) CapabilityStatus(ctx context.Context, req *nodesandboxv1.CapabilityStatusRequest) (*nodesandboxv1.CapabilityStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	status, err := s.svc.SandboxCapabilityStatus(ctx, target.targetID)
	if err != nil {
		return nil, err
	}
	return apiCapabilityStatus(status), nil
}

func apiCapabilityStatus(status service.SandboxCapabilityStatus) *nodesandboxv1.CapabilityStatusResponse {
	return &nodesandboxv1.CapabilityStatusResponse{
		Ready:           status.Ready,
		Capabilities:    append([]string(nil), status.Capabilities...),
		Providers:       apiCapabilityProviders(status.Providers),
		ProviderSummary: apiCapabilityProviderSummary(status.ProviderSummary),
	}
}

func apiCapabilityProviders(items []service.SandboxCapabilityProvider) []*nodesandboxv1.CapabilityProviderStatus {
	out := make([]*nodesandboxv1.CapabilityProviderStatus, 0, len(items))
	for _, item := range items {
		out = append(out, &nodesandboxv1.CapabilityProviderStatus{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Reason:       item.Reason,
			Dependencies: apiCapabilityDependencies(item.Dependencies),
		})
	}
	return out
}

func apiCapabilityDependencies(items []service.SandboxCapabilityDependency) []*nodesandboxv1.CapabilityDependencyStatus {
	out := make([]*nodesandboxv1.CapabilityDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, &nodesandboxv1.CapabilityDependencyStatus{
			Name:      item.Name,
			Available: item.Available,
			Reason:    item.Reason,
		})
	}
	return out
}

func apiCapabilityProviderSummary(summary service.SandboxCapabilityProviderSummary) *nodesandboxv1.CapabilityProviderSummary {
	return &nodesandboxv1.CapabilityProviderSummary{
		Total:       int32(summary.Total),
		Available:   int32(summary.Available),
		Degraded:    int32(summary.Degraded),
		Unavailable: int32(summary.Unavailable),
	}
}
