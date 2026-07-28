package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// AnthropicProvider returns a Provider for Anthropic's Messages API.
func AnthropicProvider() Provider {
	return anthropicProvider{}
}

type anthropicProvider struct{}

func (anthropicProvider) Name() string { return "anthropic" }

func (anthropicProvider) IsInferenceRequest(method string, path string) bool {
	return strings.EqualFold(method, http.MethodPost) && strings.Contains(path, "/v1/messages")
}

func (anthropicProvider) ExtractModel(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.Model
}

func (anthropicProvider) ExtractUsage(body []byte) *domain.UsageMetrics {
	if len(body) == 0 {
		return nil
	}
	if usage := extractUsageFromJSON(body); usage != nil {
		return usage
	}
	return extractUsageFromSSE(body)
}

func extractUsageFromJSON(body []byte) *domain.UsageMetrics {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	return usageFromMap(payload)
}

func extractUsageFromSSE(body []byte) *domain.UsageMetrics {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var total domain.UsageMetrics
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		if usage := usageFromMap(payload); usage != nil {
			total.InputTokens += usage.InputTokens
			total.OutputTokens += usage.OutputTokens
			total.TotalTokens += usage.TotalTokens
			total.ToolCalls += usage.ToolCalls
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

func usageFromMap(payload map[string]any) *domain.UsageMetrics {
	usageValue, ok := payload["usage"].(map[string]any)
	if !ok {
		if delta, ok := payload["delta"].(map[string]any); ok {
			usageValue, _ = delta["usage"].(map[string]any)
		}
	}
	if usageValue == nil {
		return nil
	}
	usage := domain.UsageMetrics{
		InputTokens:  intFromAny(usageValue["input_tokens"]),
		OutputTokens: intFromAny(usageValue["output_tokens"]),
		TotalTokens:  intFromAny(usageValue["total_tokens"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return &usage
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	default:
		return 0
	}
}
