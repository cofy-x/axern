package proxy

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// OpenAIProvider returns a Provider for OpenAI-compatible chat and Responses APIs.
func OpenAIProvider() Provider {
	return openaiProvider{}
}

type openaiProvider struct{}

func (openaiProvider) Name() string { return "openai" }

func (openaiProvider) IsInferenceRequest(method string, path string) bool {
	return strings.EqualFold(method, http.MethodPost) &&
		(path == "/chat/completions" ||
			path == "/responses" ||
			strings.Contains(path, "/v1/chat/completions") ||
			strings.Contains(path, "/v1/responses"))
}

func (openaiProvider) ExtractModel(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.Model
}

func (openaiProvider) ExtractUsage(body []byte) *domain.UsageMetrics {
	if len(body) == 0 {
		return nil
	}
	if usage := extractUsageFromJSON(body); usage != nil {
		return usage
	}
	return extractUsageFromSSE(body)
}
