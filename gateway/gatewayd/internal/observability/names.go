package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

const (
	SpanSSHSession             = "gateway.ssh.session"
	SpanTerminalResolve        = "gateway.terminal.resolve"
	SpanTerminalExecStreamOpen = "gateway.terminal.exec_stream.open"
)

var (
	MetricTerminalSessionsCurrent = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_sessions_current",
		Description: "Current gateway terminal sessions.",
	}
	MetricAccessGrantRetryTotal = sdkobs.Instrument{
		Name:        "axern.gateway_allocation_access_grant_retry_total",
		Description: "Gateway transient allocation access grant retries.",
	}
	MetricTerminalEventTotal = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_event_total",
		Description: "Gateway terminal lifecycle events.",
	}
	MetricTerminalResolveTotal = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_resolve_total",
		Description: "Gateway terminal resolve requests.",
	}
	MetricTerminalResolveDuration = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_resolve_duration_seconds",
		Description: "Gateway terminal resolve latency.",
	}
	MetricTerminalExecStreamOpenTotal = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_exec_stream_open_total",
		Description: "Gateway terminal exec stream opens.",
	}
	MetricTerminalExecStreamOpenDuration = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_exec_stream_open_duration_seconds",
		Description: "Gateway terminal exec stream open latency.",
	}
	MetricSSHSessionTotal = sdkobs.Instrument{
		Name:        "axern.gateway_ssh_session_total",
		Description: "Gateway SSH sessions.",
	}
	MetricSSHSessionDuration = sdkobs.Instrument{
		Name:        "axern.gateway_ssh_session_duration_seconds",
		Description: "Gateway SSH session duration.",
	}
)
