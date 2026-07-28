package llmproxy

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyRecordsAndInjectsAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(OpenAIProvider())
	proxy, err := New(Options{Upstream: upstreamURL, Provider: OpenAIProvider(), UpstreamToken: "sk-upstream", LocalToken: "local", Recorder: recorder})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()

	req, err := http.NewRequest(http.MethodPost, proxy.BaseURL()+"/responses", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if gotAuth != "Bearer sk-upstream" {
		t.Fatalf("upstream auth = %q", gotAuth)
	}
	report := recorder.Report()
	if report.RequestCount != 1 || report.ResponseCount != 1 || report.Usage == nil || report.Usage.TotalTokens != 5 {
		t.Fatalf("report = %#v", report)
	}
}

func TestRecorderNeverPersistsRequestQuery(t *testing.T) {
	recorder := NewRecorder(OpenAIProvider())
	req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/responses?api_key=secret&signature=private", strings.NewReader(`{"model":"test"}`))
	recorder.RecordRequest(req)
	report := recorder.Report()
	if len(report.Events) != 1 || report.Events[0].Path != "/v1/responses" {
		t.Fatalf("recorded path = %#v", report.Events)
	}
}

func TestStreamingProxyKeepsUsageAndBoundsTransportReport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\r\n\r\n")
		content := strings.Repeat("x", 1024)
		for range 5000 {
			payload, _ := json.Marshal(map[string]any{"type": "content_block_delta", "delta": map[string]any{"text": content}})
			_, _ = w.Write([]byte("data: " + string(payload) + "\r\n\r\n"))
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\r\n\r\n")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(AnthropicProvider())
	proxy, err := New(Options{Upstream: upstreamURL, Provider: AnthropicProvider(), UpstreamToken: "sk", LocalToken: "local", Recorder: recorder})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy.BaseURL()+"/v1/messages", strings.NewReader(`{"model":"deepseek-chat"}`))
	req.Header.Set("x-api-key", "local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	report, payload, err := recorder.MarshalTransportReport(1 << 20)
	if err != nil {
		t.Fatalf("MarshalTransportReport: %v", err)
	}
	if len(payload) > 1<<20 {
		t.Fatalf("transport report size = %d", len(payload))
	}
	if report.Usage == nil || report.Usage.InputTokens != 11 || report.Usage.OutputTokens != 3 || report.Usage.TotalTokens != 14 {
		t.Fatalf("usage = %#v", report.Usage)
	}
	if len(report.Bodies) != 0 || !report.Truncated || report.DroppedBodies == 0 || report.DroppedBytes < 5<<20 {
		t.Fatalf("bounded report = %#v", report)
	}
	for _, event := range report.Events {
		if event.Type == EventLLMChunk || event.BodyRef != "" || event.ChunkRef != "" {
			t.Fatalf("transport event retained body detail: %#v", event)
		}
	}
}

func TestMarshalTransportReportDropsOversizedLifecycleMetadata(t *testing.T) {
	recorder := NewRecorder(OpenAIProvider())
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"m"}`))
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Oversized": {strings.Repeat("x", 2<<20)}}}
	recorder.RecordResponseStart(req, resp, 1, 1)
	recorder.RecordResponseDone(req, resp, 1, 1, time.Now(), []byte(`{"usage":{"input_tokens":2,"output_tokens":3}}`), 48, nil)
	report, payload, err := recorder.MarshalTransportReport(1 << 20)
	if err != nil {
		t.Fatalf("MarshalTransportReport: %v", err)
	}
	if len(payload) > 1<<20 || !report.Truncated || report.DroppedEvents == 0 {
		t.Fatalf("report size=%d report=%#v", len(payload), report)
	}
	if report.Usage == nil || report.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", report.Usage)
	}
}

func TestStreamingProxyBoundsMalformedSSEEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", 3<<20)+"\n\n")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	recorder := NewRecorder(OpenAIProvider())
	proxy, err := New(Options{Upstream: upstreamURL, Provider: OpenAIProvider(), UpstreamToken: "sk", LocalToken: "local", Recorder: recorder})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy.BaseURL()+"/responses", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	_, payload, err := recorder.MarshalTransportReport(1 << 20)
	if err != nil || len(payload) > 1<<20 {
		t.Fatalf("bounded malformed stream report size=%d err=%v", len(payload), err)
	}
}

func TestProxyRejectsMissingLocalToken(t *testing.T) {
	upstreamURL, _ := url.Parse("https://example.invalid")
	proxy, err := New(Options{Upstream: upstreamURL, Provider: OpenAIProvider(), UpstreamToken: "sk", LocalToken: "local", Recorder: NewRecorder(OpenAIProvider())})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()
	resp, err := http.Post(proxy.BaseURL()+"/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAnthropicProxyAcceptsLocalXAPIKey(t *testing.T) {
	var gotAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(AnthropicProvider())
	proxy, err := New(Options{Upstream: upstreamURL, Provider: AnthropicProvider(), UpstreamToken: "sk-upstream", LocalToken: "local", Recorder: recorder})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()

	req, err := http.NewRequest(http.MethodPost, proxy.BaseURL()+"/v1/messages", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-api-key", "local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if gotAPIKey != "sk-upstream" {
		t.Fatalf("upstream x-api-key = %q", gotAPIKey)
	}
	if report := recorder.Report(); report.RequestCount != 1 || report.ResponseCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestInferenceDetectionIgnoresQueryString(t *testing.T) {
	provider := OpenAIProvider()
	if !provider.IsInferenceRequest(http.MethodPost, "/responses?api-version=preview") {
		t.Fatal("POST /responses with query should be inference")
	}
	if !provider.IsInferenceRequest(http.MethodPost, "/v1/chat/completions?x=1") {
		t.Fatal("POST /v1/chat/completions with query should be inference")
	}
}

func TestProxyRecordsInferenceErrorWithQueryString(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	upstreamURL, err := url.Parse("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	_ = ln.Close()
	recorder := NewRecorder(OpenAIProvider())
	proxy, err := New(Options{Upstream: upstreamURL, Provider: OpenAIProvider(), UpstreamToken: "sk", LocalToken: "local", Recorder: recorder})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer proxy.Close()

	req, err := http.NewRequest(http.MethodPost, proxy.BaseURL()+"/responses?api-version=preview", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if report := recorder.Report(); report.ErrorCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestDisableEnvironmentProxyClearsTransportProxy(t *testing.T) {
	transport, ok := defaultTransport(true).(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", defaultTransport(true))
	}
	if transport.Proxy != nil {
		t.Fatal("transport.Proxy must be nil when environment proxy is disabled")
	}
	transport, ok = defaultTransport(false).(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", defaultTransport(false))
	}
	if transport.Proxy == nil {
		t.Fatal("transport.Proxy must use environment proxy by default")
	}
}

func TestSanitizeHeadersRedactsSensitivePatterns(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret")
	headers.Set("X-Session-Token", "session-secret")
	headers.Set("X-Request-Id", "req-1")
	sanitized := SanitizeHeaders(headers)
	if sanitized["Authorization"][0] != "<redacted>" || sanitized["X-Session-Token"][0] != "<redacted>" {
		t.Fatalf("sanitized = %#v", sanitized)
	}
	if sanitized["X-Request-Id"][0] != "req-1" {
		t.Fatalf("sanitized = %#v", sanitized)
	}
}
