package observability

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestMetricsUseFixedLowSensitivityDimensions(t *testing.T) {
	eventType := reflect.TypeFor[Event]()
	wantFields := []string{"Mode", "Action", "Protocol", "Result", "AllocationID", "RuleCount", "Latency"}
	if eventType.NumField() != len(wantFields) {
		t.Fatalf("metric event fields = %d, want privacy-safe contract %#v", eventType.NumField(), wantFields)
	}
	for index, want := range wantFields {
		if got := eventType.Field(index).Name; got != want {
			t.Fatalf("metric event field %d = %q, want %q", index, got, want)
		}
	}
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
