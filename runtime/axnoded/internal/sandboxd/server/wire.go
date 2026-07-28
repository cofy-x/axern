package server

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/browser"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/computeruse"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/diagnostic"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/fileapi"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func wireStatus(status workload.StatusResponse) wire.StatusResponse {
	return wire.StatusResponse{
		ProtocolVersion: wire.ProtocolVersion,
		DaemonPID:       status.DaemonPID,
		UptimeSeconds:   status.UptimeSeconds,
		SocketPath:      status.SocketPath,
		UserProcess: wire.UserProcessStatus{
			State:      status.UserProcess.State,
			PID:        status.UserProcess.PID,
			ExitCode:   status.UserProcess.ExitCode,
			Signal:     status.UserProcess.Signal,
			StartedAt:  status.UserProcess.StartedAt,
			FinishedAt: status.UserProcess.FinishedAt,
			LastError:  status.UserProcess.LastError,
		},
	}
}

func diagnosticHTTPProbe(request *wire.HTTPProbe) *diagnostic.HTTPProbe {
	if request == nil {
		return nil
	}
	return &diagnostic.HTTPProbe{Port: request.Port, Path: request.Path, Scheme: request.Scheme}
}

func diagnosticTCPProbe(request *wire.TCPProbe) *diagnostic.TCPProbe {
	if request == nil {
		return nil
	}
	return &diagnostic.TCPProbe{Port: request.Port}
}

func wireProbeResponse(response diagnostic.ProbeResponse) wire.ProbeResponse {
	return wire.ProbeResponse{
		OK:         response.OK,
		Kind:       response.Kind,
		Target:     response.Target,
		Detail:     response.Detail,
		DurationMS: response.Duration,
	}
}

func wirePortSnapshot(snapshot diagnostic.PortSnapshot) wire.PortSnapshot {
	out := wire.PortSnapshot{Ports: make([]wire.Port, 0, len(snapshot.Ports))}
	for _, item := range snapshot.Ports {
		out.Ports = append(out.Ports, wire.Port{
			Protocol: item.Protocol,
			Address:  item.Address,
			Port:     item.Port,
			State:    item.State,
		})
	}
	return out
}

func wireMountSnapshot(snapshot diagnostic.MountSnapshot) wire.MountSnapshot {
	out := wire.MountSnapshot{
		Mounts: make([]wire.Mount, 0, len(snapshot.Mounts)),
		Paths:  make([]wire.Path, 0, len(snapshot.Paths)),
	}
	for _, item := range snapshot.Mounts {
		out.Mounts = append(out.Mounts, wire.Mount{
			Mountpoint: item.Mountpoint,
			FSType:     item.FSType,
			Source:     item.Source,
			Options:    item.Options,
		})
	}
	for _, item := range snapshot.Paths {
		out.Paths = append(out.Paths, wire.Path{
			Path:      item.Path,
			Exists:    item.Exists,
			Writable:  item.Writable,
			Total:     item.Total,
			Available: item.Available,
			Error:     item.Error,
		})
	}
	return out
}

func wireProviders(items []provider.Provider) []wire.CapabilityProvider {
	out := make([]wire.CapabilityProvider, 0, len(items))
	for _, item := range items {
		out = append(out, wire.CapabilityProvider{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Command:      item.Command,
			Reason:       item.Reason,
			LastError:    item.LastError,
			Dependencies: wireProviderDependencies(item.Dependencies),
		})
	}
	return out
}

func wireProviderSummary(summary provider.ProviderSummary) wire.ProviderSummary {
	return wire.ProviderSummary{
		Total:       summary.Total,
		Available:   summary.Available,
		Degraded:    summary.Degraded,
		Unavailable: summary.Unavailable,
	}
}

func wireProviderDependencies(items []provider.Dependency) []wire.ProviderDependency {
	out := make([]wire.ProviderDependency, 0, len(items))
	for _, item := range items {
		out = append(out, wire.ProviderDependency{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func providerFromComputerUseStatus(status computeruse.StatusResponse) provider.Provider {
	item := provider.Provider{
		Name:         provider.CapabilityComputerUse,
		Available:    status.Available,
		Capabilities: []string{provider.CapabilityComputerUse},
		Backend:      status.Backend,
		Reason:       status.Reason,
		Dependencies: providerDependenciesFromComputerUse(status.Dependencies),
	}
	if !status.Available {
		item.State = provider.ProviderStateUnavailable
	} else {
		item.State = provider.ProviderStateAvailable
	}
	return item
}

func providerDependenciesFromComputerUse(items []computeruse.DependencyStatus) []provider.Dependency {
	out := make([]provider.Dependency, 0, len(items))
	for _, item := range items {
		out = append(out, provider.Dependency{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func providerFromBrowserStatus(status browser.StatusResponse) provider.Provider {
	item := provider.Provider{
		Name:         provider.CapabilityBrowser,
		Available:    status.Available,
		Capabilities: []string{provider.CapabilityBrowser},
		Command:      status.Command,
		Reason:       status.Reason,
		LastError:    status.LastError,
	}
	switch {
	case !status.Available:
		item.State = provider.ProviderStateUnavailable
	case status.LastError != "":
		item.State = provider.ProviderStateDegraded
	default:
		item.State = provider.ProviderStateAvailable
	}
	return item
}

func wireProcessList(list process.ListResponse) wire.ProcessListResponse {
	out := wire.ProcessListResponse{Processes: make([]wire.ProcessStatus, 0, len(list.Processes))}
	for _, item := range list.Processes {
		out.Processes = append(out.Processes, wireProcessStatus(item))
	}
	return out
}

func wireProcessStatus(status process.Status) wire.ProcessStatus {
	return wire.ProcessStatus{
		ID:                 status.ID,
		State:              status.State,
		PID:                status.PID,
		ExitCode:           status.ExitCode,
		Signal:             status.Signal,
		StartedAt:          status.StartedAt,
		FinishedAt:         status.FinishedAt,
		LastError:          status.LastError,
		Stdout:             status.Stdout,
		Stderr:             status.Stderr,
		StdoutTruncated:    status.StdoutTruncated,
		StderrTruncated:    status.StderrTruncated,
		ManagedProxyReport: wireManagedProxyReport(status.ManagedProxyReport),
	}
}

func wireManagedProxyReport(report *process.ManagedProxyReport) *wire.ManagedProxyReport {
	if report == nil {
		return nil
	}
	return &wire.ManagedProxyReport{
		Provider:      report.Provider,
		RequestCount:  report.RequestCount,
		ResponseCount: report.ResponseCount,
		ErrorCount:    report.ErrorCount,
		ReportJSON:    append([]byte(nil), report.ReportJSON...),
	}
}

func wireProcessSummary(list wire.ProcessListResponse) wire.ProcessSummary {
	summary := wire.ProcessSummary{Total: len(list.Processes)}
	for _, item := range list.Processes {
		switch item.State {
		case process.ProcessStateStarting:
			summary.Starting++
		case process.ProcessStateRunning:
			summary.Running++
		case process.ProcessStateExited:
			summary.Exited++
		case process.ProcessStateFailed:
			summary.Failed++
		}
	}
	return summary
}

func wireFileLimitSnapshot(limits fileapi.LimitSnapshot) wire.FileLimitSnapshot {
	return wire.FileLimitSnapshot{
		MaxArchiveEntries:    limits.MaxArchiveEntries,
		MaxArchiveBytes:      limits.MaxArchiveBytes,
		MaxArchiveEntryBytes: limits.MaxArchiveEntryBytes,
		MaxArchivePathDepth:  limits.MaxArchivePathDepth,
	}
}

func wireComputerUseStatus(status computeruse.StatusResponse) wire.ComputerUseStatusResponse {
	return wire.ComputerUseStatusResponse{
		Available:    status.Available,
		Display:      status.Display,
		Backend:      status.Backend,
		Reason:       status.Reason,
		Dependencies: wireComputerUseDependencies(status.Dependencies),
	}
}

func wireComputerUseDependencies(items []computeruse.DependencyStatus) []wire.ComputerUseDependencyStatus {
	out := make([]wire.ComputerUseDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, wire.ComputerUseDependencyStatus{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func wireBrowserStatus(status browser.StatusResponse) wire.BrowserStatusResponse {
	return wire.BrowserStatusResponse{
		Available:    status.Available,
		Command:      status.Command,
		Running:      status.Running,
		Pid:          status.Pid,
		URL:          status.URL,
		Reason:       status.Reason,
		StartedAt:    status.StartedAt,
		LastActionAt: status.LastActionAt,
		LastError:    status.LastError,
	}
}
