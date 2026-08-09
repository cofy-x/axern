package natbench

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	axmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
)

func TestCaptureStartupSnapshotRejectsUnsupportedDebugSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(axmetrics.Snapshot{Version: "v2"})
	}))
	defer server.Close()

	_, err := CaptureStartupSnapshot(server.URL, "runsc", "local")
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("CaptureStartupSnapshot() error = %v, want unsupported version", err)
	}
}

func TestCaptureStartupSnapshotRejectsDroppedDebugRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(axmetrics.Snapshot{
			Version:        axmetrics.SnapshotVersion,
			DroppedRecords: 1,
		})
	}))
	defer server.Close()

	_, err := CaptureStartupSnapshot(server.URL, "runsc", "local")
	if err == nil || !strings.Contains(err.Error(), "records were dropped") {
		t.Fatalf("CaptureStartupSnapshot() error = %v, want dropped-record error", err)
	}
}

func TestStartupSnapshotAndDiffFromMetricsSnapshot(t *testing.T) {
	points := []axmetrics.Point{
		counterPoint(axmetrics.MetricStartupTotal, 1, attrs("cold", "runsc", "local", "ok")),
		counterPoint(axmetrics.MetricStartupTotal, 2, attrs("warm", "runsc", "local", "ok")),
		counterPoint(axmetrics.MetricStartupTotal, 1, attrs("warm", "runsc", "local", "error")),
		histogramPoint(axmetrics.MetricStartupDuration, []float64{0.08}, attrs("cold", "runsc", "local", "ok")),
		histogramPoint(axmetrics.MetricStartupDuration, []float64{0.02, 0.04}, attrs("warm", "runsc", "local", "ok")),
		histogramPoint(axmetrics.MetricStartupPhaseDuration, []float64{0.02}, phaseAttrs("resource_allocate", "cold", "runsc", "local", "ok")),
		histogramPoint(axmetrics.MetricStartupPhaseDuration, []float64{0.01, 0.01}, phaseAttrs("resource_allocate", "warm", "runsc", "local", "ok")),
		histogramPoint(axmetrics.MetricStartupPhaseDuration, []float64{0.01}, phaseAttrs("resource_allocate", "warm", "runsc", "local", "error")),
		counterPoint(axmetrics.MetricBundleTemplateTotal, 3, runtimeRootfsResultAttrs("runsc", "local", "hit")),
		counterPoint(axmetrics.MetricBundleTemplateTotal, 2, runtimeRootfsResultAttrs("runsc", "local", "miss")),
		histogramPoint(axmetrics.MetricBundleMaterializeDuration, []float64{0.01, 0.01, 0.01, 0.01, 0.01}, runtimeRootfsResultAttrs("runsc", "local", "ok")),
		counterPoint(axmetrics.MetricRuntimeWaitGraceTotal, 3, map[string]string{sdkobs.AttrRuntime: "runsc", sdkobs.AttrResult: "recovered"}),
		counterPoint(axmetrics.MetricRuntimeWaitGraceTotal, 1, map[string]string{sdkobs.AttrRuntime: "runsc", sdkobs.AttrResult: "unavailable"}),
	}

	before := &StartupSnapshot{
		Runtime:    "runsc",
		RootfsType: "local",
		Classes: map[string]StartupClassSnapshot{
			"warm": {
				OKCount:              1,
				OKDurationCount:      1,
				OKDurationSumSeconds: 0.02,
				Histogram: histogramFromSamples([]float64{
					0.02,
				}),
			},
		},
		WaitGrace: &RuntimeWaitGraceSnapshot{
			RecoveredCount: 1,
		},
		Bundle: &BundleTemplateSnapshot{
			HitCount:              1,
			MissCount:             1,
			MaterializeCount:      2,
			MaterializeSumSeconds: 0.02,
			MaterializeHistogram:  histogramFromSamples([]float64{0.01, 0.01}),
		},
	}
	after := &StartupSnapshot{
		Runtime:    "runsc",
		RootfsType: "local",
		Classes:    map[string]StartupClassSnapshot{},
	}
	collectStartupCounterSnapshot(after, points, "runsc", "local")
	collectStartupDurationSnapshot(after, points, "runsc", "local")
	collectStartupPhaseSnapshot(after, points, "runsc", "local")
	collectBundleTemplateSnapshot(after, points, "runsc", "local")
	collectBundleMaterializeSnapshot(after, points, "runsc", "local")
	collectRuntimeWaitGraceSnapshot(after, points, "runsc")

	summary := DiffStartupSummary(before, after)
	if summary == nil {
		t.Fatal("DiffStartupSummary() returned nil")
	}

	if summary.Classes["cold"].OKCount != 1 {
		t.Fatalf("cold ok count = %d, want 1", summary.Classes["cold"].OKCount)
	}
	if summary.Classes["warm"].OKCount != 1 {
		t.Fatalf("warm ok count = %d, want 1", summary.Classes["warm"].OKCount)
	}
	if summary.Classes["warm"].ErrorCount != 1 {
		t.Fatalf("warm error count = %d, want 1", summary.Classes["warm"].ErrorCount)
	}
	if summary.Classes["cold"].AverageDurationSeconds != 0.08 {
		t.Fatalf("cold average duration = %v, want 0.08", summary.Classes["cold"].AverageDurationSeconds)
	}
	if summary.Classes["cold"].Quantiles == nil {
		t.Fatal("expected cold quantiles")
	}
	if got := summary.Classes["cold"].Quantiles.P95Seconds; got != 0.08 {
		t.Fatalf("cold p95 = %v, want 0.08", got)
	}
	if got := summary.Classes["warm"].AverageDurationSeconds; got < 0.039999 || got > 0.040001 {
		t.Fatalf("warm average duration = %v, want approximately 0.04", got)
	}
	if summary.Classes["warm"].Quantiles == nil {
		t.Fatal("expected warm quantiles")
	}
	if got := summary.Classes["warm"].Quantiles.P99Seconds; got != 0.04 {
		t.Fatalf("warm p99 = %v, want 0.04", got)
	}
	resourceAllocate := summary.Phases["resource_allocate"]
	if resourceAllocate.Classes["cold"].Count != 1 {
		t.Fatalf("resource_allocate cold count = %d, want 1", resourceAllocate.Classes["cold"].Count)
	}
	if got := resourceAllocate.Classes["cold"].AverageDurationSeconds; got < 0.019999 || got > 0.020001 {
		t.Fatalf("resource_allocate cold average duration = %v, want approximately 0.02", got)
	}
	if resourceAllocate.Classes["warm"].Count != 3 {
		t.Fatalf("resource_allocate warm count = %d, want 3", resourceAllocate.Classes["warm"].Count)
	}
	if resourceAllocate.Classes["warm"].ErrorCount != 1 {
		t.Fatalf("resource_allocate warm error count = %d, want 1", resourceAllocate.Classes["warm"].ErrorCount)
	}
	if got := resourceAllocate.Classes["warm"].AverageDurationSeconds; got < 0.009999 || got > 0.010001 {
		t.Fatalf("resource_allocate warm average duration = %v, want approximately 0.01", got)
	}
	if resourceAllocate.Classes["warm"].Quantiles == nil {
		t.Fatal("expected resource_allocate warm quantiles")
	}
	if summary.WaitGrace == nil {
		t.Fatal("expected waitGrace summary")
	}
	if summary.WaitGrace.RecoveredCount != 2 {
		t.Fatalf("waitGrace recovered count = %d, want 2", summary.WaitGrace.RecoveredCount)
	}
	if summary.WaitGrace.UnavailableCount != 1 {
		t.Fatalf("waitGrace unavailable count = %d, want 1", summary.WaitGrace.UnavailableCount)
	}
	if summary.Bundle == nil {
		t.Fatal("expected bundle summary")
	}
	if summary.Bundle.HitCount != 2 {
		t.Fatalf("bundle hit count = %d, want 2", summary.Bundle.HitCount)
	}
	if summary.Bundle.MissCount != 1 {
		t.Fatalf("bundle miss count = %d, want 1", summary.Bundle.MissCount)
	}
	if got := summary.Bundle.AverageMaterializeDurationSec; got < 0.009999 || got > 0.010001 {
		t.Fatalf("bundle average materialize duration = %v, want approximately 0.01", got)
	}
	if got := summary.Bundle.HitRate; got < 0.666666 || got > 0.666667 {
		t.Fatalf("bundle hit rate = %v, want approximately 0.666666", got)
	}
	if summary.Bundle.MaterializeQuantiles == nil {
		t.Fatal("expected bundle materialize quantiles")
	}
	if summary.DominantPhaseP95["cold"] != "resource_allocate" {
		t.Fatalf("cold dominant p95 phase = %q, want resource_allocate", summary.DominantPhaseP95["cold"])
	}
	if summary.DominantPhaseP99["warm"] != "resource_allocate" {
		t.Fatalf("warm dominant p99 phase = %q, want resource_allocate", summary.DominantPhaseP99["warm"])
	}
	if summary.PhaseBreakdown["warm"]["resource_allocate"].P95Seconds == 0 {
		t.Fatal("expected warm phase breakdown p95")
	}
}

func TestHistogramBucketSnapshotJSONRoundTripInf(t *testing.T) {
	bucket := HistogramBucketSnapshot{
		UpperBound:      math.Inf(1),
		CumulativeCount: 7,
	}

	data, err := json.Marshal(bucket)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"upperBound":"inf"`) {
		t.Fatalf("marshal output = %s, want upperBound inf string", data)
	}

	var decoded HistogramBucketSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !math.IsInf(decoded.UpperBound, 1) {
		t.Fatalf("decoded upperBound = %v, want +Inf", decoded.UpperBound)
	}
	if decoded.CumulativeCount != bucket.CumulativeCount {
		t.Fatalf("decoded cumulativeCount = %d, want %d", decoded.CumulativeCount, bucket.CumulativeCount)
	}
}

func TestSubtractHistogramUsesCumulativeSampleOffsetsAfterRingWrap(t *testing.T) {
	before := histogramFromPoint(4, 4, 1, []float64{1, 1, 1})
	after := histogramFromPoint(5, 5, 2, []float64{1, 1, 1})

	delta := subtractHistogram(after, before)
	if delta == nil {
		t.Fatal("expected histogram delta")
	}
	if delta.Count != 1 || delta.SumSeconds != 1 {
		t.Fatalf("delta count/sum = %d/%v, want 1/1", delta.Count, delta.SumSeconds)
	}
	if len(delta.Samples) != 1 || delta.Samples[0] != 1 {
		t.Fatalf("delta samples = %v, want [1]", delta.Samples)
	}
	if got := histogramQuantile(delta, 0.95); got != 1 {
		t.Fatalf("delta p95 = %v, want 1", got)
	}
}

func counterPoint(name string, value float64, attrs map[string]string) axmetrics.Point {
	return axmetrics.Point{Name: name, Type: axmetrics.TypeCounter, Value: value, Attributes: attrs}
}

func histogramPoint(name string, samples []float64, attrs map[string]string) axmetrics.Point {
	sum := 0.0
	for _, sample := range samples {
		sum += sample
	}
	return axmetrics.Point{
		Name:       name,
		Type:       axmetrics.TypeHistogram,
		Count:      uint64(len(samples)),
		Sum:        sum,
		Samples:    append([]float64(nil), samples...),
		Attributes: attrs,
	}
}

func attrs(startClass, runtime, rootfsType, result string) map[string]string {
	attrs := runtimeRootfsResultAttrs(runtime, rootfsType, result)
	attrs[sdkobs.AttrStartClass] = startClass
	return attrs
}

func phaseAttrs(phase, startClass, runtime, rootfsType, result string) map[string]string {
	attrs := attrs(startClass, runtime, rootfsType, result)
	attrs[sdkobs.AttrPhase] = phase
	return attrs
}

func runtimeRootfsResultAttrs(runtime, rootfsType, result string) map[string]string {
	return map[string]string{
		sdkobs.AttrRuntime:    runtime,
		sdkobs.AttrRootFSType: rootfsType,
		sdkobs.AttrResult:     result,
	}
}
