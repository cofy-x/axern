package nodeclient

import (
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type CapabilityStatus struct {
	Ready           bool
	Capabilities    []string
	Providers       []CapabilityProviderStatus
	ProviderSummary CapabilityProviderSummary
}

type CapabilityProviderStatus struct {
	Name         string
	State        string
	Available    bool
	Capabilities []string
	Backend      string
	Reason       string
	Dependencies []CapabilityDependencyStatus
}

type CapabilityDependencyStatus struct {
	Name      string
	Available bool
	Reason    string
}

type CapabilityProviderSummary struct {
	Total       int32
	Available   int32
	Degraded    int32
	Unavailable int32
}

func (c *Client) CapabilityStatus(ctx context.Context) (CapabilityStatus, error) {
	response, err := c.nodes.CapabilityStatus(ctx, &nodesandboxv1.CapabilityStatusRequest{
		AllocationID: c.allocationID,
	})
	if err != nil {
		return CapabilityStatus{}, err
	}
	return CapabilityStatus{
		Ready:           response.GetReady(),
		Capabilities:    append([]string(nil), response.GetCapabilities()...),
		Providers:       capabilityProviders(response.GetProviders()),
		ProviderSummary: capabilityProviderSummary(response.GetProviderSummary()),
	}, nil
}

func capabilityProviders(items []*nodesandboxv1.CapabilityProviderStatus) []CapabilityProviderStatus {
	out := make([]CapabilityProviderStatus, 0, len(items))
	for _, item := range items {
		out = append(out, CapabilityProviderStatus{
			Name:         item.GetName(),
			State:        item.GetState(),
			Available:    item.GetAvailable(),
			Capabilities: append([]string(nil), item.GetCapabilities()...),
			Backend:      item.GetBackend(),
			Reason:       item.GetReason(),
			Dependencies: capabilityDependencies(item.GetDependencies()),
		})
	}
	return out
}

func capabilityDependencies(items []*nodesandboxv1.CapabilityDependencyStatus) []CapabilityDependencyStatus {
	out := make([]CapabilityDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, CapabilityDependencyStatus{
			Name:      item.GetName(),
			Available: item.GetAvailable(),
			Reason:    item.GetReason(),
		})
	}
	return out
}

func capabilityProviderSummary(summary *nodesandboxv1.CapabilityProviderSummary) CapabilityProviderSummary {
	if summary == nil {
		return CapabilityProviderSummary{}
	}
	return CapabilityProviderSummary{
		Total:       summary.GetTotal(),
		Available:   summary.GetAvailable(),
		Degraded:    summary.GetDegraded(),
		Unavailable: summary.GetUnavailable(),
	}
}
