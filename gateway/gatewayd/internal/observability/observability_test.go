package observability

import (
	"context"
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricsUseUnifiedOTelPipeline(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	obs, err := sdkobs.Init(context.Background(), sdkobs.Config{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	metrics := NewMetrics(obs)
	releaseHTTP := metrics.IncActiveHTTP()
	releaseTerminal := metrics.IncActiveTerminal()
	metrics.RouteCache("hit")
	metrics.RouteResolve("ok")
	metrics.UpstreamFailure("timeout")
	metrics.LeaseRetry("service")
	metrics.ObserveServiceProxyStage("route_resolve", "ok", "", "GET", time.Millisecond)
	metrics.TerminalEvent("open")
	finishArtifact := metrics.BeginArtifactDownload(true)
	finishArtifact(128, "ok", "none")
	releaseHTTP()
	releaseTerminal()

	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	names := make(map[string]bool)
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			names[metric.Name] = true
		}
	}
	for _, want := range []string{
		MetricHTTPRequestsCurrent.Name,
		MetricTerminalSessionsCurrent.Name,
		MetricRouteResolveTotal.Name,
		MetricRouteCacheEvents.Name,
		MetricUpstreamFailureTotal.Name,
		MetricLeaseRetryTotal.Name,
		MetricServiceProxyStageDuration.Name,
		MetricTerminalEventTotal.Name,
		MetricArtifactDownloadsCurrent.Name,
		MetricArtifactDownloadsTotal.Name,
		MetricArtifactDownloadBytesTotal.Name,
		MetricArtifactDownloadDuration.Name,
	} {
		if !names[want] {
			t.Fatalf("OTel metrics missing %q: %v", want, names)
		}
	}
}
