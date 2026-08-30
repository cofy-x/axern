package qualification

import (
	"strings"
	"testing"
)

func TestComparePassesStableCandidateAndRejectsRegressions(t *testing.T) {
	baseline := testReport(t)
	candidate := testReport(t)
	candidate.Subject.Commit = strings.Repeat("c", 40)
	budget := testBudget()
	comparison, err := Compare(baseline, candidate, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Comparable || !comparison.Passed || len(comparison.Violations) != 0 {
		t.Fatalf("stable comparison = %#v", comparison)
	}
	candidate.Scenarios[0].Metrics.PrepareLatencyMS.P95 *= 2
	candidate.Scenarios[0].Metrics.PrepareLatencyMS.P99 *= 2
	candidate.Scenarios[0].Metrics.PrepareLatencyMS.Max *= 2
	candidate.Scenarios[0].Metrics.Failures = 1
	comparison, err = Compare(baseline, candidate, budget)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Passed || len(comparison.Violations) != 3 {
		t.Fatalf("regressed comparison = %#v", comparison)
	}
}

func TestCompareRefusesDifferentEnvironmentOrParameters(t *testing.T) {
	baseline := testReport(t)
	candidate := testReport(t)
	candidate.Environment.KernelRelease = "different"
	candidate.Environment.EnvironmentID, _ = candidate.Environment.Fingerprint()
	if _, err := Compare(baseline, candidate, testBudget()); err == nil || !strings.Contains(err.Error(), "environment provenance differs") {
		t.Fatalf("environment comparison = %v", err)
	}
	candidate = testReport(t)
	candidate.Parameters.Concurrency++
	if _, err := Compare(baseline, candidate, testBudget()); err == nil || !strings.Contains(err.Error(), "parameters differ") {
		t.Fatalf("parameter comparison = %v", err)
	}
}

func TestCompareRejectsMetricAvailabilityDrift(t *testing.T) {
	baseline := testReport(t)
	candidate := testReport(t)
	candidate.Scenarios[0].Metrics.DNSLatencyMS = nil
	comparison, err := Compare(baseline, candidate, testBudget())
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Passed || len(comparison.Violations) != 1 || !strings.Contains(comparison.Violations[0].Metric, "availability") {
		t.Fatalf("availability comparison = %#v", comparison)
	}
}

func testBudget() Budget {
	return Budget{
		SchemaVersion: SchemaVersion, MaxLatencyP95Ratio: 1.20, MaxLatencyP99Ratio: 1.30,
		MinThroughputRatio: 0.85, MaxRSSRatio: 1.15, MinConcurrencyRatio: 0.90,
		MaxFailureRate: 0, MaxRuleScaleLatencyRatio: 1.25, MaxRuleScaleRSSRatio: 1.20,
	}
}
