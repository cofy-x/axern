package proxy

import (
	"net/http"
	"testing"
)

func TestAnthropicProviderName(t *testing.T) {
	p := AnthropicProvider()
	if p.Name() != "anthropic" {
		t.Fatalf("Name() = %q", p.Name())
	}
}

func TestAnthropicProviderInferenceDetection(t *testing.T) {
	p := AnthropicProvider()
	if !p.IsInferenceRequest(http.MethodPost, "/v1/messages") {
		t.Fatal("POST /v1/messages should be inference")
	}
	if !p.IsInferenceRequest(http.MethodPost, "/api/v1/messages") {
		t.Fatal("POST /api/v1/messages should be inference")
	}
	if p.IsInferenceRequest(http.MethodGet, "/v1/messages") {
		t.Fatal("GET /v1/messages should not be inference")
	}
	if p.IsInferenceRequest(http.MethodPost, "/v1/chat/completions") {
		t.Fatal("POST /v1/chat/completions should not be Anthropic inference")
	}
}

func TestAnthropicProviderModelExtraction(t *testing.T) {
	p := AnthropicProvider()
	if model := p.ExtractModel([]byte(`{"model":"claude-3"}`)); model != "claude-3" {
		t.Fatalf("model = %q", model)
	}
	if model := p.ExtractModel([]byte(`{}`)); model != "" {
		t.Fatalf("empty model = %q", model)
	}
	if model := p.ExtractModel(nil); model != "" {
		t.Fatalf("nil model = %q", model)
	}
}

func TestAnthropicProviderUsageExtraction(t *testing.T) {
	p := AnthropicProvider()
	usage := p.ExtractUsage([]byte(`{"usage":{"input_tokens":10,"output_tokens":20}}`))
	if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 20 || usage.TotalTokens != 30 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestAnthropicProviderSSEUsageExtraction(t *testing.T) {
	p := AnthropicProvider()
	sse := []byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n")
	usage := p.ExtractUsage(sse)
	if usage == nil || usage.OutputTokens != 5 {
		t.Fatalf("sse usage = %#v", usage)
	}
}

func TestOpenAIProviderName(t *testing.T) {
	p := OpenAIProvider()
	if p.Name() != "openai" {
		t.Fatalf("Name() = %q", p.Name())
	}
}

func TestOpenAIProviderInferenceDetection(t *testing.T) {
	p := OpenAIProvider()
	if !p.IsInferenceRequest(http.MethodPost, "/v1/chat/completions") {
		t.Fatal("POST /v1/chat/completions should be inference")
	}
	if !p.IsInferenceRequest(http.MethodPost, "/v1/responses") {
		t.Fatal("POST /v1/responses should be inference")
	}
	if !p.IsInferenceRequest(http.MethodPost, "/responses") {
		t.Fatal("POST /responses should be inference")
	}
	if p.IsInferenceRequest(http.MethodGet, "/v1/chat/completions") {
		t.Fatal("GET should not be inference")
	}
	if p.IsInferenceRequest(http.MethodPost, "/v1/messages") {
		t.Fatal("POST /v1/messages should not be OpenAI inference")
	}
}

func TestOpenAIProviderModelExtraction(t *testing.T) {
	p := OpenAIProvider()
	if model := p.ExtractModel([]byte(`{"model":"gpt-4o"}`)); model != "gpt-4o" {
		t.Fatalf("model = %q", model)
	}
}

func TestOpenAIProviderUsageExtraction(t *testing.T) {
	p := OpenAIProvider()
	usage := p.ExtractUsage([]byte(`{"usage":{"input_tokens":5,"output_tokens":15}}`))
	if usage == nil || usage.TotalTokens != 20 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestHeaderSanitization(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer secret")
	h.Set("x-api-key", "key")
	h.Set("Cookie", "sid=abc")
	h.Set("X-Request-Id", "req-1")
	h.Set("X-Session-Token", "tok")

	sanitized := SanitizeHeaders(h)
	if _, ok := sanitized["Authorization"]; ok {
		t.Fatal("Authorization should be stripped")
	}
	if _, ok := sanitized["X-Api-Key"]; ok {
		t.Fatal("x-api-key should be stripped")
	}
	if _, ok := sanitized["Cookie"]; ok {
		t.Fatal("Cookie should be stripped")
	}
	if _, ok := sanitized["X-Session-Token"]; ok {
		t.Fatal("session header should be stripped")
	}
	if _, ok := sanitized["Content-Type"]; !ok {
		t.Fatal("Content-Type should be preserved")
	}
	if _, ok := sanitized["X-Request-Id"]; !ok {
		t.Fatal("X-Request-Id should be preserved")
	}
}
