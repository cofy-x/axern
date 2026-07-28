package proxy

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestEstimateCostKnownModel(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 1000, OutputTokens: 500}
	cost := EstimateCost("claude-sonnet-4-20250514", usage)
	if cost == nil {
		t.Fatal("EstimateCost returned nil for known model")
	}
	if cost.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", cost.Currency)
	}
	expectedInput := 1000.0 * 3.0 / 1_000_000
	expectedOutput := 500.0 * 15.0 / 1_000_000
	expected := expectedInput + expectedOutput
	if cost.Amount < expected*0.99 || cost.Amount > expected*1.01 {
		t.Fatalf("amount = %f, want ~%f", cost.Amount, expected)
	}
}

func TestEstimateCostUnknownModel(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 1000, OutputTokens: 500}
	cost := EstimateCost("unknown-model-xyz", usage)
	if cost != nil {
		t.Fatalf("EstimateCost returned %v for unknown model, want nil", cost)
	}
}

func TestEstimateCostNilUsage(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4", nil)
	if cost != nil {
		t.Fatalf("EstimateCost returned %v for nil usage, want nil", cost)
	}
}

func TestEstimateCostZeroTokens(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 0, OutputTokens: 0}
	cost := EstimateCost("claude-sonnet-4", usage)
	if cost != nil {
		t.Fatalf("EstimateCost returned %v for zero tokens, want nil", cost)
	}
}

func TestEstimateCostCaseInsensitive(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 1000, OutputTokens: 500}
	cost := EstimateCost("Claude-Sonnet-4-Latest", usage)
	if cost == nil {
		t.Fatal("EstimateCost returned nil for case-variant model")
	}
}

func TestEstimateCostDeepseekPrefix(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 10000, OutputTokens: 5000}
	cost := EstimateCost("deepseek-chat", usage)
	if cost == nil {
		t.Fatal("EstimateCost returned nil for deepseek-chat")
	}
	if cost.Amount <= 0 {
		t.Fatalf("amount = %f, want > 0", cost.Amount)
	}
}

func TestEstimateCostLongestPrefixWins(t *testing.T) {
	usage := &domain.UsageMetrics{InputTokens: 1_000_000, OutputTokens: 0}
	cost := EstimateCost("o3-mini-2025-04-01", usage)
	if cost == nil {
		t.Fatal("EstimateCost returned nil for o3-mini variant")
	}
	expected := 1.10
	if cost.Amount < expected*0.99 || cost.Amount > expected*1.01 {
		t.Fatalf("amount = %f, want ~%f (o3-mini pricing, not o3)", cost.Amount, expected)
	}
}
