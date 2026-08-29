package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestMetricsUseFixedLowSensitivityDimensions(t *testing.T) {
	metrics, err := NewMetrics(noop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	metrics.Record(context.Background(), Event{
		Mode: ModeStrict, Action: ActionAllow, Protocol: ProtocolHTTPS,
		Result: ResultOK, AllocationID: "alloc-1", RuleCount: 3, Latency: time.Millisecond,
	})
	if validMode(Mode("github.com")) != "unknown" || validProtocol(Protocol("host.example")) != "unknown" {
		t.Fatal("free-form domain data was accepted as a metric dimension")
	}
}
