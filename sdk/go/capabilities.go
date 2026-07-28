package axernsdk

import (
	"context"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
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

func (s *Sandbox) CapabilityStatus(ctx context.Context) (CapabilityStatus, error) {
	node, err := s.nodeClient()
	if err != nil {
		return CapabilityStatus{}, err
	}
	return node.CapabilityStatus(ctx)
}

func (n *NodeSandboxClient) CapabilityStatus(ctx context.Context) (CapabilityStatus, error) {
	if err := n.validate(); err != nil {
		return CapabilityStatus{}, err
	}
	status, err := n.rpcClient().CapabilityStatus(ctx)
	if err != nil {
		return CapabilityStatus{}, mapRPCError(err, "sandbox capability status", n.allocationID)
	}
	return CapabilityStatus{
		Ready:           status.Ready,
		Capabilities:    append([]string(nil), status.Capabilities...),
		Providers:       sdkCapabilityProviders(status.Providers),
		ProviderSummary: sdkCapabilityProviderSummary(status.ProviderSummary),
	}, nil
}

func sdkCapabilityProviders(items []nodeclient.CapabilityProviderStatus) []CapabilityProviderStatus {
	out := make([]CapabilityProviderStatus, 0, len(items))
	for _, item := range items {
		out = append(out, CapabilityProviderStatus{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Reason:       item.Reason,
			Dependencies: sdkCapabilityDependencies(item.Dependencies),
		})
	}
	return out
}

func sdkCapabilityDependencies(items []nodeclient.CapabilityDependencyStatus) []CapabilityDependencyStatus {
	out := make([]CapabilityDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, CapabilityDependencyStatus{
			Name:      item.Name,
			Available: item.Available,
			Reason:    item.Reason,
		})
	}
	return out
}

func sdkCapabilityProviderSummary(summary nodeclient.CapabilityProviderSummary) CapabilityProviderSummary {
	return CapabilityProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}
