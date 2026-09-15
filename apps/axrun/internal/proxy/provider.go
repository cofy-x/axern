package proxy

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

// Provider defines provider-specific response parsing used by Axrun when it
// imports agent telemetry. Provider authentication and launch configuration are
// also Axrun concerns; Axern only supplies the Allocation execution boundary.
type Provider interface {
	// Name returns the provider identifier (e.g. "anthropic", "openai").
	Name() string

	// IsInferenceRequest reports whether the request is an LLM inference
	// call that should be counted toward LLMRequestCount/LLMResponseCount.
	IsInferenceRequest(method string, path string) bool

	// ExtractModel reads the model identifier from a request body.
	ExtractModel(body []byte) string

	// ExtractUsage reads token usage from a response body or SSE chunk.
	ExtractUsage(body []byte) *domain.UsageMetrics
}
