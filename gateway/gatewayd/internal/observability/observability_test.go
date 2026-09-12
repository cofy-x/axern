package observability

import (
	"context"
	"testing"

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
	releaseTerminal := metrics.IncActiveTerminal()
	metrics.LeaseRetry("terminal")
	metrics.TerminalEvent("open")
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
		MetricTerminalSessionsCurrent.Name,
		MetricLeaseRetryTotal.Name,
		MetricTerminalEventTotal.Name,
	} {
		if !names[want] {
			t.Fatalf("OTel metrics missing %q: %v", want, names)
		}
	}
}
