package observability

import (
	"slices"
	"testing"
)

func TestDefaultDurationHistogramBucketsResolveStartupLatency(t *testing.T) {
	want := []float64{
		0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1,
		0.25, 0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3, 4, 5, 7.5,
		10, 30, 60,
	}
	if !slices.Equal(defaultDurationHistogramBuckets, want) {
		t.Fatalf("duration histogram buckets = %v, want %v", defaultDurationHistogramBuckets, want)
	}
}

func TestHistogramCacheKeyIncludesExplicitBoundaries(t *testing.T) {
	first := histogramCacheKey("latency", "description", []float64{0.1, 0.25, 0.5})
	second := histogramCacheKey("latency", "description", []float64{0.1, 0.2, 0.5})
	if first == second {
		t.Fatal("histogram cache key does not distinguish explicit boundaries")
	}
	if got := histogramCacheKey("latency", "description", []float64{0.1, 0.25, 0.5}); got != first {
		t.Fatalf("histogram cache key = %q, want stable key %q", got, first)
	}
}
