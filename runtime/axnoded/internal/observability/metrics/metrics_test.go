package metrics

import (
	"strings"
	"testing"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func TestRecordExecutionLeaseVisibilityUsesBoundedResultLabel(t *testing.T) {
	ResetForTest()
	RecordExecutionLeaseVisibility(25*time.Millisecond, "event_wait")

	if got := HistogramCountForTest(MetricExecutionLeaseVisibilityDuration, map[string]string{"axern.result": "event_wait"}); got != 1 {
		t.Fatalf("histogram count = %d, want 1", got)
	}
}

func TestRecordResourceAllocateStageUsesBoundedDimensions(t *testing.T) {
	ResetForTest()
	RecordResourceAllocateStage("interface", "validate_cached", "ok", 0.025)
	flushResourceAllocateStagesForTest()

	if got := HistogramCountForTest(MetricResourceAllocateStageDuration, map[string]string{
		"axern.resource": "interface",
		"axern.stage":    "validate_cached",
		"axern.result":   "ok",
	}); got != 1 {
		t.Fatalf("histogram count = %d, want 1", got)
	}
}

func TestSnapshotCurrentKeepsBoundedHistogramSamplesInOrder(t *testing.T) {
	ResetForTest()
	attrs := []attribute.KeyValue{attribute.String("test.key", "value")}
	for i := 0; i < maxDebugHistogramSamples+3; i++ {
		debugRecorder.addHistogramSample("test.histogram", attrs, float64(i))
	}

	snapshot := SnapshotCurrent()
	if len(snapshot.Points) != 1 {
		t.Fatalf("point count = %d, want 1", len(snapshot.Points))
	}
	point := snapshot.Points[0]
	if point.Count != maxDebugHistogramSamples+3 {
		t.Fatalf("histogram count = %d, want %d", point.Count, maxDebugHistogramSamples+3)
	}
	if point.SampleStart != 3 {
		t.Fatalf("sample start = %d, want 3", point.SampleStart)
	}
	if len(point.Samples) != maxDebugHistogramSamples {
		t.Fatalf("sample count = %d, want %d", len(point.Samples), maxDebugHistogramSamples)
	}
	if point.Samples[0] != 3 {
		t.Fatalf("first sample = %v, want 3", point.Samples[0])
	}
	if want := float64(maxDebugHistogramSamples + 2); point.Samples[len(point.Samples)-1] != want {
		t.Fatalf("last sample = %v, want %v", point.Samples[len(point.Samples)-1], want)
	}
}

func TestSnapshotCurrentSeparatesAttributeSets(t *testing.T) {
	ResetForTest()
	debugRecorder.addCounter("test.counter", []attribute.KeyValue{
		attribute.String("b", "2"),
		attribute.String("a", "1"),
	}, 1)
	debugRecorder.addCounter("test.counter", []attribute.KeyValue{
		attribute.String("a", "1"),
		attribute.String("b", "2"),
	}, 2)

	point, ok := debugRecorder.find(TypeCounter, "test.counter", map[string]string{"a": "1", "b": "2"})
	if !ok {
		t.Fatal("counter point not found")
	}
	if point.Value != 3 {
		t.Fatalf("counter value = %v, want 3", point.Value)
	}
}

func TestSnapshotCurrentAttributeKeysCannotCollide(t *testing.T) {
	ResetForTest()
	debugRecorder.addCounter("test.counter", []attribute.KeyValue{
		attribute.String("a", "b\nc=d"),
	}, 1)
	debugRecorder.addCounter("test.counter", []attribute.KeyValue{
		attribute.String("a", "b"),
		attribute.String("c", "d"),
	}, 1)

	snapshot := SnapshotCurrent()
	if len(snapshot.Points) != 2 {
		t.Fatalf("point count = %d, want 2", len(snapshot.Points))
	}
}

func TestSnapshotCurrentBoundsSeriesAndReportsDroppedRecords(t *testing.T) {
	ResetForTest()
	for i := 0; i < maxDebugSeries+2; i++ {
		debugRecorder.addCounter("test.counter", []attribute.KeyValue{
			attribute.Int("series", i),
		}, 1)
	}

	snapshot := SnapshotCurrent()
	if len(snapshot.Points) != maxDebugSeries {
		t.Fatalf("point count = %d, want %d", len(snapshot.Points), maxDebugSeries)
	}
	if snapshot.DroppedRecords != 2 {
		t.Fatalf("dropped records = %d, want 2", snapshot.DroppedRecords)
	}
}

func TestSnapshotCurrentSanitizesAttributeValues(t *testing.T) {
	ResetForTest()
	debugRecorder.addCounter("test.counter", []attribute.KeyValue{
		attribute.String("value", strings.Repeat("x", 300)),
	}, 1)

	snapshot := SnapshotCurrent()
	if len(snapshot.Points) != 1 {
		t.Fatalf("point count = %d, want 1", len(snapshot.Points))
	}
	got := snapshot.Points[0].Attributes["value"]
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("attribute value was not sanitized: length=%d", len(got))
	}
}

func TestCapabilityGovernanceMetricsUseBoundedDimensions(t *testing.T) {
	ResetForTest()
	RecordCapabilityRecoveryDebounce("PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT", "CAPABILITY_PROVIDER_RUNTIME_CONFORMANCE")
	RecordCapabilityFailStopCleanup("runsc", "retry")

	snapshot := SnapshotCurrent()
	if len(snapshot.Points) != 2 {
		t.Fatalf("point count = %d, want 2", len(snapshot.Points))
	}
	for _, point := range snapshot.Points {
		switch point.Name {
		case MetricCapabilityRecoveryDebounceTotal:
			if point.Attributes["capability"] == "" || point.Attributes["provider"] == "" {
				t.Fatalf("recovery debounce attributes = %#v", point.Attributes)
			}
		case MetricCapabilityFailStopCleanupTotal:
			if point.Attributes[sdkobs.AttrRuntime] != "runsc" || point.Attributes[sdkobs.AttrResult] != "retry" {
				t.Fatalf("fail-stop cleanup attributes = %#v", point.Attributes)
			}
		default:
			t.Fatalf("unexpected metric %q", point.Name)
		}
	}
}
