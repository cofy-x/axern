package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

const (
	SpanServiceProxy           = "gateway.service.proxy"
	SpanRouteResolve           = "gateway.route.resolve"
	SpanSSHSession             = "gateway.ssh.session"
	SpanTerminalResolve        = "gateway.terminal.resolve"
	SpanTerminalExecStreamOpen = "gateway.terminal.exec_stream.open"
)

var (
	MetricServiceProxyRequests = sdkobs.Instrument{
		Name:        "axern.gateway_service_proxy_requests_total",
		Description: "Gateway service proxy requests.",
	}
	MetricServiceProxyDuration = sdkobs.Instrument{
		Name:        "axern.gateway_service_proxy_duration_seconds",
		Description: "Gateway service proxy latency.",
	}
	MetricHTTPRequestsCurrent = sdkobs.Instrument{
		Name:        "axern.gateway_http_requests_current",
		Description: "Current gateway HTTP service requests.",
	}
	MetricTerminalSessionsCurrent = sdkobs.Instrument{
		Name:        "axern.gateway_terminal_sessions_current",
		Description: "Current gateway terminal sessions.",
	}
	MetricRouteResolveTotal = sdkobs.Instrument{
		Name:        "axern.gateway_route_resolve_total",
		Description: "Gateway route resolve attempts.",
	}
	MetricUpstreamFailureTotal = sdkobs.Instrument{
		Name:        "axern.gateway_upstream_failure_total",
		Description: "Gateway upstream failures.",
	}
	MetricLeaseRetryTotal = sdkobs.Instrument{
		Name:        "axern.gateway_lease_retry_total",
		Description: "Gateway transient execution lease retries.",
	}
	MetricServiceProxyStageDuration = sdkobs.Instrument{
		Name:        "axern.gateway_service_proxy_stage_duration_seconds",
		Description: "Gateway service proxy stage duration.",
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
	MetricRouteCacheEvents = sdkobs.Instrument{
		Name:        "axern.gateway_route_cache_events_total",
		Description: "Gateway route cache events.",
	}
	MetricRouteCacheEntriesCurrent = sdkobs.Instrument{
		Name:        "axern.gateway_route_cache_entries_current",
		Description: "Current bounded gateway route cache state.",
	}
	MetricArtifactDownloadsCurrent = sdkobs.Instrument{
		Name:        "axern.gateway_artifact_downloads_current",
		Description: "Current gateway artifact downloads.",
	}
	MetricArtifactDownloadsTotal = sdkobs.Instrument{
		Name:        "axern.gateway_artifact_downloads_total",
		Description: "Gateway artifact download results.",
	}
	MetricArtifactDownloadBytesTotal = sdkobs.Instrument{
		Name:        "axern.gateway_artifact_download_bytes_total",
		Description: "Artifact bytes streamed through the gateway.",
	}
	MetricArtifactDownloadDuration = sdkobs.Instrument{
		Name:        "axern.gateway_artifact_download_duration_seconds",
		Description: "Gateway artifact download duration.",
	}
)
