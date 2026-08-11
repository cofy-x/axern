package sandbox

import (
	"fmt"
	"io"
	"strings"

	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
)

func renderSandboxDiagnostics(w io.Writer, diagnostics *nodeoperatorv1.GetSandboxDiagnosticsResponse) {
	if diagnostics == nil {
		return
	}
	fmt.Fprintf(w, "Sandbox: %s\n", diagnostics.GetSandboxID())
	fmt.Fprintf(w, "Ready: %t\n", diagnostics.GetReady())
	fmt.Fprintf(w, "Detail: %s\n", fallbackString(diagnostics.GetDetail(), "summary"))
	fmt.Fprintf(w, "Generated At: %s\n", formatTimestamp(diagnostics.GetGeneratedAt()))
	fmt.Fprintf(w, "Daemon PID: %s\n", formatPID(diagnostics.GetDaemonPid()))
	fmt.Fprintf(w, "Uptime Seconds: %.3f\n", diagnostics.GetUptimeSeconds())
	fmt.Fprintf(w, "Socket: %s\n", fallbackString(diagnostics.GetSocketPath(), "-"))
	fmt.Fprintf(w, "User State: %s\n", fallbackString(diagnostics.GetUserState(), "-"))
	fmt.Fprintf(w, "Capabilities: %s\n", joinOrDash(diagnostics.GetCapabilities()))
	if summary := diagnostics.GetProviderSummary(); summary != nil {
		fmt.Fprintf(w, "Providers: %d total, %d available, %d degraded, %d unavailable\n", summary.GetTotal(), summary.GetAvailable(), summary.GetDegraded(), summary.GetUnavailable())
	}
	if summary := diagnostics.GetProcessSummary(); summary != nil {
		fmt.Fprintf(w, "Processes: %d total, %d starting, %d running, %d exited, %d failed\n", summary.GetTotal(), summary.GetStarting(), summary.GetRunning(), summary.GetExited(), summary.GetFailed())
	}
	if len(diagnostics.GetProviders()) > 0 {
		fmt.Fprintln(w, "Provider Details:")
		for _, provider := range diagnostics.GetProviders() {
			renderSandboxDiagnosticProvider(w, provider)
		}
	}
	if diagnostics.GetMemory() != nil {
		fmt.Fprintln(w, "Memory:")
		renderSandboxMemory(w, diagnostics.GetMemory())
	}
}

func renderSandboxDiagnosticProvider(w io.Writer, provider *nodeoperatorv1.SandboxdProvider) {
	if provider == nil {
		return
	}
	state := fallbackString(provider.GetState(), "-")
	fmt.Fprintf(w, "  - %s: state=%s available=%t capabilities=%s\n", provider.GetName(), state, provider.GetAvailable(), joinOrDash(provider.GetCapabilities()))
	if backend := strings.TrimSpace(provider.GetBackend()); backend != "" {
		fmt.Fprintf(w, "    backend: %s\n", backend)
	}
	if command := strings.TrimSpace(provider.GetCommand()); command != "" {
		fmt.Fprintf(w, "    command: %s\n", command)
	}
	if reason := strings.TrimSpace(provider.GetReason()); reason != "" {
		fmt.Fprintf(w, "    reason: %s\n", reason)
	}
	if lastError := strings.TrimSpace(provider.GetLastError()); lastError != "" {
		fmt.Fprintf(w, "    last error: %s\n", lastError)
	}
	for _, dependency := range provider.GetDependencies() {
		if dependency == nil {
			continue
		}
		line := fmt.Sprintf("    dependency %s: available=%t", dependency.GetName(), dependency.GetAvailable())
		if reason := strings.TrimSpace(dependency.GetReason()); reason != "" {
			line += " reason=" + reason
		}
		fmt.Fprintln(w, line)
	}
}

func fallbackString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}
