package proxy

import (
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// modelPrice holds per-million-token pricing for input and output.
type modelPrice struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// knownPricing maps model ID prefixes to pricing. Prices are in USD
// per million tokens. This is a best-effort table; unknown models
// return nil cost and never cause errors.
var knownPricing = map[string]modelPrice{
	"claude-sonnet-4":   {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-3-5-sonnet": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-3-7-sonnet": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
	"claude-opus-4":     {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-3-opus":     {InputPerMillion: 15.0, OutputPerMillion: 75.0},
	"claude-3-5-haiku":  {InputPerMillion: 0.8, OutputPerMillion: 4.0},
	"claude-3-haiku":    {InputPerMillion: 0.25, OutputPerMillion: 1.25},
	"deepseek-chat":     {InputPerMillion: 0.27, OutputPerMillion: 1.10},
	"deepseek-reasoner": {InputPerMillion: 0.55, OutputPerMillion: 2.19},
	"deepseek-v4":       {InputPerMillion: 0.27, OutputPerMillion: 1.10},
	"gpt-4o":            {InputPerMillion: 2.50, OutputPerMillion: 10.0},
	"gpt-4o-mini":       {InputPerMillion: 0.15, OutputPerMillion: 0.60},
	"gpt-4.1":           {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"gpt-4.1-mini":      {InputPerMillion: 0.40, OutputPerMillion: 1.60},
	"gpt-4.1-nano":      {InputPerMillion: 0.10, OutputPerMillion: 0.40},
	"o3":                {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	"o3-mini":           {InputPerMillion: 1.10, OutputPerMillion: 4.40},
	"o4-mini":           {InputPerMillion: 1.10, OutputPerMillion: 4.40},
}

// EstimateCost computes a best-effort cost estimate from token usage
// and a known model ID. Returns nil for unknown models.
func EstimateCost(modelID string, usage *domain.UsageMetrics) *domain.CostMetrics {
	if usage == nil || modelID == "" {
		return nil
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return nil
	}
	price, ok := lookupPrice(modelID)
	if !ok {
		return nil
	}
	amount := float64(usage.InputTokens)*price.InputPerMillion/1_000_000 +
		float64(usage.OutputTokens)*price.OutputPerMillion/1_000_000
	if amount <= 0 {
		return nil
	}
	return &domain.CostMetrics{
		Amount:   amount,
		Currency: "USD",
	}
}

func lookupPrice(modelID string) (modelPrice, bool) {
	lower := strings.ToLower(modelID)
	if p, ok := knownPricing[lower]; ok {
		return p, true
	}
	var bestPrefix string
	var bestPrice modelPrice
	for prefix, p := range knownPricing {
		if strings.HasPrefix(lower, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestPrice = p
		}
	}
	if bestPrefix != "" {
		return bestPrice, true
	}
	return modelPrice{}, false
}
