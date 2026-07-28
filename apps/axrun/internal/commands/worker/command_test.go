package worker

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestTestPricingEstimatorIsExplicitAndIsolated(t *testing.T) {
	estimator, err := testPricingEstimator(`{"mock-scripted-agent":{"input_per_million":1,"output_per_million":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := estimator("mock-scripted-agent", &domain.UsageMetrics{InputTokens: 10, OutputTokens: 5}); got == nil || got.Amount != 0.00002 {
		t.Fatalf("cost = %#v, want USD 0.00002", got)
	}
	if got := estimator("production-unknown", &domain.UsageMetrics{InputTokens: 1}); got != nil {
		t.Fatalf("unknown model cost = %#v, want nil", got)
	}
	if estimator, err := testPricingEstimator(""); err != nil || estimator != nil {
		t.Fatalf("empty test config returned estimator=%t err=%v, want false and nil", estimator != nil, err)
	}
}

func TestTestPricingEstimatorRejectsUnknownFields(t *testing.T) {
	if _, err := testPricingEstimator(`{"mock":{"input":1}}`); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}
