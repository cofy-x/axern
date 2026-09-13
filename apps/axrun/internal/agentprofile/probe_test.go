package agentprofile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestProbeResponsesAPI(t *testing.T) {
	var path, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"gpt-test"`) {
			t.Fatalf("body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-test","output":[]}`))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "gpt-test",
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if !result.Reachable || !result.Compatible || path != "/v1/responses" || authorization != "Bearer secret-token" {
		t.Fatalf("result=%+v path=%q authorization=%q", result, path, authorization)
	}
}

func TestProbeAnthropicMessagesAPI(t *testing.T) {
	var path, apiKey, version string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/anthropic", AgentClaudeCode, ProviderAnthropic, WireAPIAnthropicMessages),
		Model:   "claude-test",
	})
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if !result.Compatible || path != "/anthropic/v1/messages" || apiKey != "secret-token" || version == "" {
		t.Fatalf("result=%+v path=%q apiKey=%q version=%q", result, path, apiKey, version)
	}
}

func TestProbeClassifiesUnsupportedResponsesEndpoint(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "deepseek-chat",
	})
	if err == nil || result.ErrorClass != ProbeErrorUnsupportedProtocol || result.Retryable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Endpoint, "secret-token") {
		t.Fatalf("endpoint leaked credentials: %q", result.Endpoint)
	}
}

func TestProbeDistinguishesModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"model not found"}}`))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "missing",
	})
	if err == nil || result.ErrorClass != ProbeErrorModelNotFound {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeFailureMatrix(t *testing.T) {
	tests := []struct {
		status    int
		class     string
		retryable bool
	}{
		{http.StatusUnauthorized, ProbeErrorAuthentication, false},
		{http.StatusForbidden, ProbeErrorPermission, false},
		{http.StatusNotFound, ProbeErrorUnsupportedProtocol, false},
		{http.StatusTooManyRequests, ProbeErrorRateLimited, true},
		{http.StatusInternalServerError, ProbeErrorUnavailable, true},
		{http.StatusServiceUnavailable, ProbeErrorUnavailable, true},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":{"message":"deterministic failure"}}`))
			}))
			defer server.Close()
			result, err := Probe(context.Background(), ProbeRequest{Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses), Model: "mock"})
			if err == nil || result.ErrorClass != test.class || result.Retryable != test.retryable {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestProbeTimeoutIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":"late"}`))
	}))
	defer server.Close()
	result, err := Probe(context.Background(), ProbeRequest{Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses), Model: "mock", Timeout: 10 * time.Millisecond})
	if err == nil || result.ErrorClass != ProbeErrorTimeout || !result.Retryable || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeMissingUsageUsesExplicitFallbackSignal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"resp-no-usage","output":[]}`))
	}))
	defer server.Close()
	result, err := Probe(context.Background(), ProbeRequest{Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses), Model: "mock"})
	if err != nil || result.UsageReported || result.InputTokens != 0 || result.OutputTokens != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeRejectsDisconnectedResponseBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(strings.NewReader(`{"id":`), errorReader{})), Header: make(http.Header)}, nil
	})}
	result, err := Probe(context.Background(), ProbeRequest{Profile: probeProfile(t, "https://provider.test/v1", AgentCodex, ProviderOpenAI, WireAPIResponses), Model: "mock", Client: client})
	if err == nil || result.ErrorClass != ProbeErrorInvalidResponse {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestProbeRejectsNonJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "gpt-test",
	})
	if err == nil || result.ErrorClass != ProbeErrorInvalidResponse || result.Compatible {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeRejectsUnrelatedJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "gpt-test",
	})
	if err == nil || result.ErrorClass != ProbeErrorInvalidResponse || result.Compatible {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Probe(ctx, ProbeRequest{
		Profile: probeProfile(t, "https://api.example.test/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "gpt-test",
	})
	if err == nil || result.ErrorClass != ProbeErrorCanceled || result.Retryable || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeClassifiesModelNotFoundFromBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model does not exist"}}`))
	}))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
		Model:   "missing",
	})
	if err == nil || result.ErrorClass != ProbeErrorModelNotFound {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestProbeRequiresModelWithoutSendingRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()

	result, err := Probe(context.Background(), ProbeRequest{
		Profile: probeProfile(t, server.URL+"/v1", AgentCodex, ProviderOpenAI, WireAPIResponses),
	})
	if err == nil || result.ErrorClass != ProbeErrorInvalidConfig || called {
		t.Fatalf("result=%+v err=%v called=%t", result, err, called)
	}
}

func TestProbePreservesUpstreamQueryButRedactsDiagnostics(t *testing.T) {
	var query string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query = request.URL.RawQuery
		return nil, errors.New("dial https://api.example.test/v1/responses?secret=query-token: unavailable")
	})}
	profile := probeProfile(t, "https://api.example.test/v1?secret=query-token", AgentCodex, ProviderOpenAI, WireAPIResponses)
	result, err := Probe(context.Background(), ProbeRequest{Profile: profile, Model: "gpt-test", Client: client})
	if err == nil || result.ErrorClass != ProbeErrorUnavailable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if query != "secret=query-token" {
		t.Fatalf("query = %q", query)
	}
	if strings.Contains(result.Endpoint, "query-token") || strings.Contains(err.Error(), "query-token") {
		t.Fatalf("probe diagnostics leaked upstream query: result=%+v err=%v", result, err)
	}
}

func probeProfile(t *testing.T, rawURL string, agent AgentType, provider ProviderType, wireAPI WireAPI) Profile {
	t.Helper()
	upstream, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return Profile{
		Name:         "test-profile",
		Agent:        agent,
		ProviderType: provider,
		WireAPI:      wireAPI,
		Upstream:     upstream,
		Token:        "secret-token",
	}
}
