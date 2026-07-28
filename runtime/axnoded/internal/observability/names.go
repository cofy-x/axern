package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

const (
	SpanHTTP                         = "axnoded.http"
	SpanAllocationStart              = "axnoded.allocation.start"
	SpanAllocationDelete             = "axnoded.allocation.delete"
	SpanControlPlaneRegister         = "axnoded.control_plane.register"
	SpanControlPlaneReportNode       = "axnoded.control_plane.report_node"
	SpanControlPlaneReportAllocation = "axnoded.control_plane.report_allocation_status"
	SpanRootFSPrepare                = "axnoded.rootfs.prepare"
	SpanExec                         = "axnoded.exec"
	SpanExecStream                   = "axnoded.exec_stream"
	SpanHTTPProxy                    = "axnoded.http_proxy"
	SpanResourceAllocate             = "axnoded.resource.allocate"
	SpanRuntimeCreate                = "axnoded.runtime.create"
	SpanStatusReport                 = "axnoded.status.report"
)

var (
	MetricAllocationStartTotal = sdkobs.Instrument{
		Name:        "axern.axnoded_allocation_start_total",
		Description: "Axnoded allocation start requests.",
	}
	MetricAllocationStartDuration = sdkobs.Instrument{
		Name:        "axern.axnoded_allocation_start_duration_seconds",
		Description: "Axnoded allocation start latency.",
	}
	MetricAllocationDeleteTotal = sdkobs.Instrument{
		Name:        "axern.axnoded_allocation_delete_total",
		Description: "Axnoded allocation delete requests.",
	}
	MetricAllocationDeleteDuration = sdkobs.Instrument{
		Name:        "axern.axnoded_allocation_delete_duration_seconds",
		Description: "Axnoded allocation delete latency.",
	}
	MetricExecTotal = sdkobs.Instrument{
		Name:        "axern.axnoded_exec_total",
		Description: "Axnoded exec requests.",
	}
	MetricExecDuration = sdkobs.Instrument{
		Name:        "axern.axnoded_exec_duration_seconds",
		Description: "Axnoded exec latency.",
	}
	MetricHTTPProxyTotal = sdkobs.Instrument{
		Name:        "axern.axnoded_http_proxy_total",
		Description: "Axnoded HTTP proxy streams.",
	}
	MetricHTTPProxyDuration = sdkobs.Instrument{
		Name:        "axern.axnoded_http_proxy_duration_seconds",
		Description: "Axnoded HTTP proxy duration.",
	}
	MetricControlPlaneReportTotal = sdkobs.Instrument{
		Name:        "axern.axnoded_control_plane_report_total",
		Description: "Axnoded control-plane report attempts.",
	}
	MetricControlPlaneReportDuration = sdkobs.Instrument{
		Name:        "axern.axnoded_control_plane_report_duration_seconds",
		Description: "Axnoded control-plane report latency.",
	}
)
