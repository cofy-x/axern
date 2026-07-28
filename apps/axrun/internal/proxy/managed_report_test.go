package proxy

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/llmproxy"
)

func TestImportManagedProxyReportWritesArtifactsAndCounts(t *testing.T) {
	recorder, artifactDir := newTestRecorder(t, OpenAIProvider())
	now := time.Now().UTC()
	report := llmproxy.Report{
		Provider:      "openai",
		RequestCount:  1,
		ResponseCount: 1,
		Usage:         &llmproxy.Usage{InputTokens: 3, OutputTokens: 5, TotalTokens: 8},
		Bodies: []llmproxy.BodyCapture{
			{Ref: "request-000001.body", Data: []byte(`{"model":"gpt-4o"}`)},
			{Ref: "response-000001.body", Data: []byte(`{"usage":{"input_tokens":3,"output_tokens":5}}`)},
		},
		Events: []llmproxy.Event{
			{
				Type:       llmproxy.EventLLMRequest,
				Timestamp:  now,
				Method:     http.MethodPost,
				Path:       "/v1/responses",
				Model:      "gpt-4o",
				BodyRef:    "request-000001.body",
				RequestRef: "req-000001",
			},
			{
				Type:        llmproxy.EventLLMDone,
				Timestamp:   now,
				Method:      http.MethodPost,
				Path:        "/v1/responses",
				Status:      http.StatusOK,
				BodyRef:     "response-000001.body",
				RequestRef:  "req-000001",
				ResponseRef: "resp-000001",
				Usage:       &llmproxy.Usage{InputTokens: 3, OutputTokens: 5, TotalTokens: 8},
			},
			{
				Type:          llmproxy.EventLLMTruncated,
				Timestamp:     now,
				DroppedEvents: 7,
				DroppedBodies: 3,
				DroppedBytes:  4096,
			},
		},
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := ImportManagedProxyReport(recorder, payload); err != nil {
		t.Fatalf("ImportManagedProxyReport: %v", err)
	}
	result, err := recorder.Result()
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if result.RequestCount != 1 || result.ResponseCount != 1 || result.Usage == nil || result.Usage.TotalTokens != 8 {
		t.Fatalf("result = %#v", result)
	}
	assertFileExists(t, filepath.Join(artifactDir, "llm", "request-000001.body"))
	assertFileExists(t, filepath.Join(artifactDir, "llm", "response-000001.body"))
	events := readRawEvents(t, artifactDir)
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if events[0]["body_ref"] != "episodes/episode-1/artifacts/llm/request-000001.body" {
		t.Fatalf("request body ref = %#v", events[0])
	}
	if events[2]["type"] != string(domain.AgentRawEventLLMTruncated) || events[2]["dropped_events"] != float64(7) || events[2]["dropped_bodies"] != float64(3) || events[2]["dropped_bytes"] != float64(4096) {
		t.Fatalf("truncation event = %#v", events[2])
	}
}

func TestFinalizeAgentResultSetsProxyNoRequestsExitReason(t *testing.T) {
	recorder, _ := newTestRecorder(t, AnthropicProvider())
	recorder.mergeManagedProxyCounts(llmproxy.Report{ResponseCount: 1})
	result := domain.AgentResult{Status: domain.AgentStatusCompleted, ExitReason: domain.AgentExitReasonCompleted}
	if err := FinalizeAgentResult(recorder, &result); err != nil {
		t.Fatalf("FinalizeAgentResult returned error: %v", err)
	}
	if result.ExitReason != domain.AgentExitReasonProxyNoRequests {
		t.Fatalf("exit reason = %q, want %q", result.ExitReason, domain.AgentExitReasonProxyNoRequests)
	}
}

func TestFinalizeAgentResultSetsLLMErrorExitReason(t *testing.T) {
	recorder, _ := newTestRecorder(t, AnthropicProvider())
	recorder.mergeManagedProxyCounts(llmproxy.Report{RequestCount: 1, ErrorCount: 2})
	result := domain.AgentResult{Status: domain.AgentStatusCompleted, ExitReason: domain.AgentExitReasonCompleted}
	if err := FinalizeAgentResult(recorder, &result); err != nil {
		t.Fatalf("FinalizeAgentResult returned error: %v", err)
	}
	if result.ExitReason != domain.AgentExitReasonLLMError {
		t.Fatalf("exit reason = %q, want %q", result.ExitReason, domain.AgentExitReasonLLMError)
	}
}

func TestRecorderReturnsWriteFailure(t *testing.T) {
	recorder, artifactDir := newTestRecorder(t, AnthropicProvider())
	if err := os.Remove(filepath.Join(artifactDir, agentRawLogFilename)); err != nil {
		t.Fatalf("remove raw log: %v", err)
	}
	if err := os.Mkdir(filepath.Join(artifactDir, agentRawLogFilename), 0o755); err != nil {
		t.Fatalf("mkdir raw log path: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m"}`))
	recorder.RecordRequest(req)
	if _, err := recorder.Result(); err == nil || !strings.Contains(err.Error(), "agent raw log") {
		t.Fatalf("telemetry error = %v", err)
	}
}

func TestProducerTagReflectsProvider(t *testing.T) {
	for _, tc := range []struct {
		provider Provider
		want     string
	}{
		{AnthropicProvider(), "anthropic-proxy"},
		{OpenAIProvider(), "openai-proxy"},
	} {
		recorder, _ := newTestRecorder(t, tc.provider)
		result, err := recorder.Result()
		if err != nil {
			t.Fatalf("result: %v", err)
		}
		if len(result.Artifacts) == 0 || result.Artifacts[0].Producer != tc.want {
			t.Fatalf("producer for %s = %q, want %q", tc.provider.Name(), result.Artifacts[0].Producer, tc.want)
		}
	}
}

func TestRecorderRedactsSensitiveRawEventFields(t *testing.T) {
	recorder, artifactDir := newTestRecorder(t, OpenAIProvider())
	recorder.AppendEvent(domain.AgentRawEvent{
		Type:        domain.AgentRawEventCommandStarted,
		Path:        "/v1/chat/completions?api_key=sk-test-secret",
		Command:     []string{"tool", "--api-key", "sk-test-secret", "TOKEN=secret-value"},
		CommandText: `curl -H "Authorization: Bearer sk-test-secret" https://example.test?token=secret-value`,
		Error:       "failed with password=secret-value",
		Headers: map[string][]string{
			"Authorization": {"Bearer sk-test-secret"},
			"Content-Type":  {"application/json"},
		},
	})

	events := readRawEvents(t, artifactDir)
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	payload, err := json.Marshal(events[0])
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, "sk-test-secret") || strings.Contains(text, "secret-value") {
		t.Fatalf("raw event was not redacted: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("raw event missing redaction marker: %s", text)
	}
	headers, _ := events[0]["headers"].(map[string]any)
	if _, ok := headers["Authorization"]; ok {
		t.Fatalf("sensitive header should be removed: %#v", headers)
	}
}

func newTestRecorder(t *testing.T, provider Provider) (*Recorder, string) {
	t.Helper()
	artifactDir := filepath.Join(t.TempDir(), "runs", "run-1", "episodes", "episode-1", "artifacts")
	recorder, err := NewRecorder(artifactDir, provider)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	return recorder, artifactDir
}

func readRawEvents(t *testing.T, artifactDir string) []map[string]any {
	t.Helper()
	file, err := os.Open(filepath.Join(artifactDir, agentRawLogFilename))
	if err != nil {
		t.Fatalf("open raw log: %v", err)
	}
	defer file.Close()
	var events []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan raw log: %v", err)
	}
	return events
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}
