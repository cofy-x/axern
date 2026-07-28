package service

import "github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"

func sandboxCapabilityStatusFromSandboxAccess(status sandboxaccess.SandboxCapabilityStatus) SandboxCapabilityStatus {
	return SandboxCapabilityStatus{
		Ready:           status.Ready,
		Capabilities:    append([]string(nil), status.Capabilities...),
		Providers:       sandboxCapabilityProvidersFromSandboxAccess(status.Providers),
		ProviderSummary: sandboxCapabilityProviderSummaryFromSandboxAccess(status.ProviderSummary),
	}
}

func sandboxCapabilityProvidersFromSandboxAccess(items []sandboxaccess.SandboxCapabilityProvider) []SandboxCapabilityProvider {
	out := make([]SandboxCapabilityProvider, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxCapabilityProvider{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Reason:       item.Reason,
			Dependencies: sandboxCapabilityDependenciesFromSandboxAccess(item.Dependencies),
		})
	}
	return out
}

func sandboxCapabilityDependenciesFromSandboxAccess(items []sandboxaccess.SandboxCapabilityDependency) []SandboxCapabilityDependency {
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

func sandboxCapabilityProviderSummaryFromSandboxAccess(summary sandboxaccess.SandboxCapabilityProviderSummary) SandboxCapabilityProviderSummary {
	return SandboxCapabilityProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}

func sandboxdDiagnosticsFromSandboxAccess(diagnostics sandboxaccess.SandboxdDiagnostics) SandboxdDiagnostics {
	return SandboxdDiagnostics{
		GeneratedAt: diagnostics.GeneratedAt,
		Ready:       diagnostics.Ready,
		Detail:      diagnostics.Detail,
		Status: SandboxdDiagnosticsStatus{
			DaemonPID:     diagnostics.Status.DaemonPID,
			UptimeSeconds: diagnostics.Status.UptimeSeconds,
			SocketPath:    diagnostics.Status.SocketPath,
			UserState:     diagnostics.Status.UserState,
		},
		Capabilities:    append([]string(nil), diagnostics.Capabilities...),
		Providers:       sandboxdProvidersFromSandboxAccess(diagnostics.Providers),
		ProviderSummary: sandboxdProviderSummaryFromSandboxAccess(diagnostics.ProviderSummary),
		ProcessSummary:  sandboxdProcessSummaryFromSandboxAccess(diagnostics.ProcessSummary),
		RawJSON:         diagnostics.RawJSON,
	}
}

func sandboxdProvidersFromSandboxAccess(items []sandboxaccess.SandboxdProvider) []SandboxdProvider {
	out := make([]SandboxdProvider, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxdProvider{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Command:      item.Command,
			Reason:       item.Reason,
			LastError:    item.LastError,
			Dependencies: sandboxdProviderDependenciesFromSandboxAccess(item.Dependencies),
		})
	}
	return out
}

func sandboxdProviderDependenciesFromSandboxAccess(items []sandboxaccess.SandboxdProviderDependency) []SandboxdProviderDependency {
	out := make([]SandboxdProviderDependency, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxdProviderDependency{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func sandboxdProviderSummaryFromSandboxAccess(summary sandboxaccess.SandboxdProviderSummary) SandboxdProviderSummary {
	return SandboxdProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}

func sandboxdProcessSummaryFromSandboxAccess(summary sandboxaccess.SandboxdProcessSummary) SandboxdProcessSummary {
	return SandboxdProcessSummary{
		Total:    summary.Total,
		Starting: summary.Starting,
		Running:  summary.Running,
		Exited:   summary.Exited,
		Failed:   summary.Failed,
	}
}
