package sandboxaccess

import (
	"context"
	"fmt"
)

type SandboxCapabilityStatus struct {
	Ready           bool
	Capabilities    []string
	Providers       []SandboxCapabilityProvider
	ProviderSummary SandboxCapabilityProviderSummary
}

type SandboxCapabilityProvider struct {
	Name         string
	State        string
	Available    bool
	Capabilities []string
	Backend      string
	Reason       string
	Dependencies []SandboxCapabilityDependency
}

type SandboxCapabilityDependency struct {
	Name      string
	Available bool
	Reason    string
}

type SandboxCapabilityProviderSummary struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

func (a *Accessor) SandboxCapabilityStatus(ctx context.Context, containerID string) (SandboxCapabilityStatus, error) {
	if containerID == "" {
		return SandboxCapabilityStatus{}, fmt.Errorf("sandbox capability status requires container id")
	}
	diagnostics, err := a.SandboxdDiagnostics(ctx, containerID, false)
	if err != nil {
		return SandboxCapabilityStatus{}, err
	}
	return SandboxCapabilityStatusFromDiagnostics(diagnostics), nil
}

func SandboxCapabilityStatusFromDiagnostics(diagnostics SandboxdDiagnostics) SandboxCapabilityStatus {
	return SandboxCapabilityStatus{
		Ready:           diagnostics.Ready,
		Capabilities:    append([]string(nil), diagnostics.Capabilities...),
		Providers:       sandboxCapabilityProvidersFromDiagnostics(diagnostics.Providers),
		ProviderSummary: sandboxCapabilityProviderSummaryFromDiagnostics(diagnostics.ProviderSummary),
	}
}

func sandboxCapabilityProvidersFromDiagnostics(items []SandboxdProvider) []SandboxCapabilityProvider {
	out := make([]SandboxCapabilityProvider, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxCapabilityProvider{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Reason:       item.Reason,
			Dependencies: sandboxCapabilityDependenciesFromDiagnostics(item.Dependencies),
		})
	}
	return out
}

func sandboxCapabilityDependenciesFromDiagnostics(items []SandboxdProviderDependency) []SandboxCapabilityDependency {
	out := make([]SandboxCapabilityDependency, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxCapabilityDependency{
			Name:      item.Name,
			Available: item.Available,
			Reason:    item.Reason,
		})
	}
	return out
}

func sandboxCapabilityProviderSummaryFromDiagnostics(summary SandboxdProviderSummary) SandboxCapabilityProviderSummary {
	return SandboxCapabilityProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}
