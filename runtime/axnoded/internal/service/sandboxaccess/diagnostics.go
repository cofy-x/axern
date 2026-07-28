package sandboxaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

type SandboxdDiagnostics struct {
	GeneratedAt     time.Time
	Ready           bool
	Detail          string
	Status          SandboxdDiagnosticsStatus
	Capabilities    []string
	Providers       []SandboxdProvider
	ProviderSummary SandboxdProviderSummary
	ProcessSummary  SandboxdProcessSummary
	RawJSON         string
}

type SandboxdDiagnosticsStatus struct {
	DaemonPID     int
	UptimeSeconds float64
	SocketPath    string
	UserState     string
}

type SandboxdProvider struct {
	Name         string
	State        string
	Available    bool
	Capabilities []string
	Backend      string
	Command      string
	Reason       string
	LastError    string
	Dependencies []SandboxdProviderDependency
}

type SandboxdProviderDependency struct {
	Name      string
	Available bool
	Reason    string
}

type SandboxdProviderSummary struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

type SandboxdProcessSummary struct {
	Total    int
	Starting int
	Running  int
	Exited   int
	Failed   int
}

func (a *Accessor) SandboxdDiagnostics(ctx context.Context, id string, full bool) (SandboxdDiagnostics, error) {
	if id == "" {
		return SandboxdDiagnostics{}, fmt.Errorf("sandboxd diagnostics requires container id")
	}
	diagnostics, err := a.DiagnosticsWire(ctx, id, full)
	if err != nil {
		return SandboxdDiagnostics{}, err
	}
	return SandboxdDiagnosticsFromWire(diagnostics)
}

func SandboxdDiagnosticsFromWire(diagnostics wire.DiagnosticsResponse) (SandboxdDiagnostics, error) {
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		return SandboxdDiagnostics{}, fmt.Errorf("marshal sandboxd diagnostics: %w", err)
	}
	return SandboxdDiagnostics{
		GeneratedAt: diagnostics.GeneratedAt,
		Ready:       diagnostics.Ready,
		Detail:      diagnostics.Detail,
		Status: SandboxdDiagnosticsStatus{
			DaemonPID:     diagnostics.Status.DaemonPID,
			UptimeSeconds: diagnostics.Status.UptimeSeconds,
			SocketPath:    diagnostics.Status.SocketPath,
			UserState:     diagnostics.Status.UserProcess.State,
		},
		Capabilities:    append([]string(nil), diagnostics.Capabilities...),
		Providers:       sandboxdProvidersFromWire(diagnostics.Providers),
		ProviderSummary: sandboxdProviderSummaryFromWire(diagnostics.ProviderSummary),
		ProcessSummary:  sandboxdProcessSummaryFromWire(diagnostics.ProcessSummary),
		RawJSON:         string(raw),
	}, nil
}

func sandboxdProvidersFromWire(items []wire.CapabilityProvider) []SandboxdProvider {
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
			Dependencies: sandboxdProviderDependenciesFromWire(item.Dependencies),
		})
	}
	return out
}

func sandboxdProviderDependenciesFromWire(items []wire.ProviderDependency) []SandboxdProviderDependency {
	out := make([]SandboxdProviderDependency, 0, len(items))
	for _, item := range items {
		out = append(out, SandboxdProviderDependency{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func sandboxdProviderSummaryFromWire(summary wire.ProviderSummary) SandboxdProviderSummary {
	return SandboxdProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}

func sandboxdProcessSummaryFromWire(summary wire.ProcessSummary) SandboxdProcessSummary {
	return SandboxdProcessSummary{
		Total:    summary.Total,
		Starting: summary.Starting,
		Running:  summary.Running,
		Exited:   summary.Exited,
		Failed:   summary.Failed,
	}
}
