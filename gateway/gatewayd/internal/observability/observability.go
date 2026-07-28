package observability

import (
	"context"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Metrics struct {
	activeHTTP        sdkobs.UpDownCounter
	activeTerminal    sdkobs.UpDownCounter
	routeResolves     sdkobs.Counter
	routeCache        sdkobs.Counter
	routeCacheEntries sdkobs.Gauge
	upstreamFailure   sdkobs.Counter
	leaseRetries      sdkobs.Counter
	serviceStages     sdkobs.Histogram
	terminalEvents    sdkobs.Counter
	artifactActive    sdkobs.UpDownCounter
	artifactDownloads sdkobs.Counter
	artifactBytes     sdkobs.Counter
	artifactDuration  sdkobs.Histogram
}

func NewMetrics(obs *sdkobs.Handle) *Metrics {
	if obs == nil {
		return &Metrics{}
	}
	return &Metrics{
		activeHTTP:        obs.Int64UpDownCounter(MetricHTTPRequestsCurrent.Name, MetricHTTPRequestsCurrent.Description),
		activeTerminal:    obs.Int64UpDownCounter(MetricTerminalSessionsCurrent.Name, MetricTerminalSessionsCurrent.Description),
		routeResolves:     obs.Int64Counter(MetricRouteResolveTotal.Name, MetricRouteResolveTotal.Description),
		routeCache:        obs.Int64Counter(MetricRouteCacheEvents.Name, MetricRouteCacheEvents.Description),
		routeCacheEntries: obs.Float64Gauge(MetricRouteCacheEntriesCurrent.Name, MetricRouteCacheEntriesCurrent.Description),
		upstreamFailure:   obs.Int64Counter(MetricUpstreamFailureTotal.Name, MetricUpstreamFailureTotal.Description),
		leaseRetries:      obs.Int64Counter(MetricLeaseRetryTotal.Name, MetricLeaseRetryTotal.Description),
		serviceStages:     obs.DurationHistogram(MetricServiceProxyStageDuration.Name, MetricServiceProxyStageDuration.Description),
		terminalEvents:    obs.Int64Counter(MetricTerminalEventTotal.Name, MetricTerminalEventTotal.Description),
		artifactActive:    obs.Int64UpDownCounter(MetricArtifactDownloadsCurrent.Name, MetricArtifactDownloadsCurrent.Description),
		artifactDownloads: obs.Int64Counter(MetricArtifactDownloadsTotal.Name, MetricArtifactDownloadsTotal.Description),
		artifactBytes:     obs.Int64Counter(MetricArtifactDownloadBytesTotal.Name, MetricArtifactDownloadBytesTotal.Description),
		artifactDuration:  obs.DurationHistogram(MetricArtifactDownloadDuration.Name, MetricArtifactDownloadDuration.Description),
	}
}

func (m *Metrics) BeginArtifactDownload(resumed bool) func(int64, string, string) {
	if m == nil {
		return func(int64, string, string) {}
	}
	started := time.Now()
	m.artifactActive.Add(context.Background(), 1)
	return func(bytes int64, result, errorClass string) {
		m.artifactActive.Add(context.Background(), -1)
		attributes := []attribute.KeyValue{
			attribute.String(sdkobs.AttrResult, normalizeLabel(result, "unknown")),
			attribute.String(sdkobs.AttrErrorClass, normalizeLabel(errorClass, "none")),
			attribute.Bool("resume", resumed),
		}
		m.artifactDownloads.Add(context.Background(), 1, attributes...)
		if bytes > 0 {
			m.artifactBytes.Add(context.Background(), bytes, attribute.Bool("resume", resumed))
		}
		m.artifactDuration.RecordDuration(context.Background(), time.Since(started), attributes...)
	}
}

func (m *Metrics) RouteCacheEntries(state string, value int) {
	if m == nil {
		return
	}
	m.routeCacheEntries.Record(
		context.Background(),
		float64(value),
		attribute.String(sdkobs.AttrState, normalizeLabel(state, "unknown")),
	)
}

func (m *Metrics) IncActiveHTTP() func() {
	if m == nil {
		return func() {}
	}
	m.activeHTTP.Add(context.Background(), 1)
	return func() {
		m.activeHTTP.Add(context.Background(), -1)
	}
}

func (m *Metrics) IncActiveTerminal() func() {
	if m == nil {
		return func() {}
	}
	m.activeTerminal.Add(context.Background(), 1)
	return func() {
		m.activeTerminal.Add(context.Background(), -1)
	}
}

func (m *Metrics) RouteResolve(result string) {
	if m == nil {
		return
	}
	m.routeResolves.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrResult, normalizeLabel(result, "unknown")),
	)
}

func (m *Metrics) RouteCache(event string) {
	if m == nil {
		return
	}
	m.routeCache.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrEvent, normalizeLabel(event, "unknown")),
	)
}

func (m *Metrics) UpstreamFailure(errorClass string) {
	if m == nil {
		return
	}
	m.upstreamFailure.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrErrorClass, normalizeLabel(errorClass, "unknown")),
	)
}

func (m *Metrics) LeaseRetry(routeType string) {
	if m == nil {
		return
	}
	m.leaseRetries.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrRouteType, normalizeLabel(routeType, "unknown")),
	)
}

func (m *Metrics) ObserveServiceProxyStage(stage, result, errorClass, method string, duration time.Duration) {
	if m == nil {
		return
	}
	m.serviceStages.RecordDuration(
		context.Background(),
		duration,
		attribute.String(sdkobs.AttrStage, normalizeLabel(stage, "unknown")),
		attribute.String(sdkobs.AttrResult, normalizeLabel(result, "unknown")),
		attribute.String(sdkobs.AttrErrorClass, normalizeLabel(errorClass, "none")),
		attribute.String(sdkobs.AttrHTTPMethod, normalizeLabel(method, "unknown")),
	)
}

func (m *Metrics) TerminalEvent(event string) {
	if m == nil {
		return
	}
	m.terminalEvents.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrEvent, normalizeLabel(event, "unknown")),
	)
}
