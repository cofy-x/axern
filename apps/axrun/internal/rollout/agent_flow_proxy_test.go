package rollout

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
)

func TestFinalizeAgentResultSetsProxyNoRequestsExitReason(t *testing.T) {
	recorder, err := proxy.NewRecorder(t.TempDir(), proxyTestProvider{})
	if err != nil {
		t.Fatalf("NewRecorder returned error: %v", err)
	}
	recorder.RecordError(string(domain.AgentRawEventLLMError), http.MethodPost, "/v1/messages", errors.New("upstream unavailable"))

	result := domain.AgentResult{Status: domain.AgentStatusCompleted, ExitReason: domain.AgentExitReasonCompleted}
	if err := proxy.FinalizeAgentResult(recorder, &result); err != nil {
		t.Fatalf("FinalizeAgentResult returned error: %v", err)
	}

	if result.ExitReason != domain.AgentExitReasonProxyNoRequests {
		t.Fatalf("exit_reason = %q, want proxy_no_requests", result.ExitReason)
	}
	if result.LLMErrorCount != 1 {
		t.Fatalf("llm_error_count = %d, want 1", result.LLMErrorCount)
	}
}

func TestFinalizeAgentResultSetsLLMErrorExitReason(t *testing.T) {
	recorder, err := proxy.NewRecorder(t.TempDir(), proxyTestProvider{})
	if err != nil {
		t.Fatalf("NewRecorder returned error: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", strings.NewReader(`{"model":"test/model"}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if recorder.RecordRequest(req) == 0 {
		t.Fatal("RecordRequest returned zero request id")
	}
	recorder.RecordError(string(domain.AgentRawEventLLMError), http.MethodPost, "/v1/messages", errors.New("upstream unavailable"))

	result := domain.AgentResult{Status: domain.AgentStatusCompleted, ExitReason: domain.AgentExitReasonCompleted}
	if err := proxy.FinalizeAgentResult(recorder, &result); err != nil {
		t.Fatalf("FinalizeAgentResult returned error: %v", err)
	}

	if result.ExitReason != domain.AgentExitReasonLLMError {
		t.Fatalf("exit_reason = %q, want llm_error", result.ExitReason)
	}
	if result.LLMRequestCount != 1 || result.LLMErrorCount != 1 {
		t.Fatalf("result = %#v", result)
	}
}

type proxyTestProvider struct{}

func (proxyTestProvider) Name() string { return "test" }

func (proxyTestProvider) IsInferenceRequest(method string, path string) bool {
	return method == http.MethodPost && strings.Contains(path, "/v1/messages")
}

func (proxyTestProvider) InjectAuth(header http.Header, token string) {
	header.Set("Authorization", "Bearer "+token)
}

func (proxyTestProvider) ExtractModel(body []byte) string {
	if strings.Contains(string(body), "test/model") {
		return "test/model"
	}
	return ""
}

func (proxyTestProvider) ExtractUsage([]byte) *domain.UsageMetrics { return nil }
