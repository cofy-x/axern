package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

const (
	SpanHTTP         = "imagemgr.http"
	SpanOCIMount     = "imagemgr.oci.mount"
	SpanOCIUnmount   = "imagemgr.oci.unmount"
	SpanOCIImport    = "imagemgr.oci.import"
	SpanNydusMount   = "imagemgr.nydus.mount"
	SpanNydusUnmount = "imagemgr.nydus.unmount"
	SpanOSSMount     = "imagemgr.oss.mount"
	SpanOSSUnmount   = "imagemgr.oss.unmount"
	SpanInventory    = "imagemgr.inventory"
)

var (
	MetricHTTPOperationTotal = sdkobs.Instrument{
		Name:        "axern.imagemgr_http_operation_total",
		Description: "Imagemgr HTTP operation requests.",
	}
	MetricHTTPOperationDuration = sdkobs.Instrument{
		Name:        "axern.imagemgr_http_operation_duration_seconds",
		Description: "Imagemgr HTTP operation latency.",
	}
	MetricTimedOperationTotal = sdkobs.Instrument{
		Name:        "axern.imagemgr_timed_operation_total",
		Description: "Imagemgr timed operation attempts.",
	}
	MetricTimedOperationDuration = sdkobs.Instrument{
		Name:        "axern.imagemgr_timed_operation_duration_seconds",
		Description: "Imagemgr timed operation latency.",
	}
	MetricTimedOperationStageDuration = sdkobs.Instrument{
		Name:        "axern.imagemgr_timed_operation_stage_duration_seconds",
		Description: "Imagemgr timed operation stage latency.",
	}
)
