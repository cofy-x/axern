package llmproxy

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type Provider interface {
	Name() string
	IsInferenceRequest(method string, path string) bool
	InjectAuth(header http.Header, token string)
	ExtractModel(body []byte) string
	ExtractUsage(body []byte) *Usage
}

type Usage struct {
	InputTokens     int64 `json:"inputTokens,omitempty"`
	OutputTokens    int64 `json:"outputTokens,omitempty"`
	CacheReadTokens int64 `json:"cacheReadTokens,omitempty"`
	TotalTokens     int64 `json:"totalTokens,omitempty"`
}

func OpenAIProvider() Provider {
	return openaiProvider{}
}

type openaiProvider struct{}

func (openaiProvider) Name() string { return "openai" }

func (openaiProvider) IsInferenceRequest(method string, path string) bool {
	path = cleanPath(path)
	return strings.EqualFold(method, http.MethodPost) &&
		(path == "/chat/completions" ||
			path == "/responses" ||
			strings.Contains(path, "/v1/chat/completions") ||
			strings.Contains(path, "/v1/responses"))
}

func (openaiProvider) InjectAuth(header http.Header, token string) {
	header.Set("Authorization", "Bearer "+token)
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

func (openaiProvider) ExtractUsage(body []byte) *Usage {
	return extractUsage(body)
}

func AnthropicProvider() Provider {
	return anthropicProvider{}
}

type anthropicProvider struct{}

func (anthropicProvider) Name() string { return "anthropic" }

func (anthropicProvider) IsInferenceRequest(method string, path string) bool {
	path = cleanPath(path)
	return strings.EqualFold(method, http.MethodPost) && strings.Contains(path, "/v1/messages")
}

func (anthropicProvider) InjectAuth(header http.Header, token string) {
	header.Set("x-api-key", token)
	header.Set("anthropic-version", "2023-06-01")
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

func (anthropicProvider) ExtractUsage(body []byte) *Usage {
	return extractUsage(body)
}

func ProviderForName(name string) Provider {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "openai":
		return OpenAIProvider()
	case "anthropic":
		return AnthropicProvider()
	default:
		return nil
	}
}

func cleanPath(path string) string {
	if parsed, err := url.ParseRequestURI(path); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	if index := strings.IndexByte(path, '?'); index >= 0 {
		return path[:index]
	}
	return path
}
