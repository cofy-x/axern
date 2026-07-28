package agentprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ProbeErrorInvalidConfig       = "invalid_config"
	ProbeErrorUnsupportedProtocol = "unsupported_protocol"
	ProbeErrorAuthentication      = "authentication_failed"
	ProbeErrorPermission          = "permission_denied"
	ProbeErrorModelNotFound       = "model_not_found"
	ProbeErrorRateLimited         = "rate_limited"
	ProbeErrorCanceled            = "canceled"
	ProbeErrorTimeout             = "timeout"
	ProbeErrorUnavailable         = "unavailable"
	ProbeErrorInvalidResponse     = "invalid_response"
)

const defaultProbeTimeout = 15 * time.Second

type ProbeRequest struct {
	Profile Profile
	Model   string
	Client  *http.Client
	Timeout time.Duration
}

type ProbeResult struct {
	WireAPI       string `json:"wire_api"`
	Endpoint      string `json:"endpoint"`
	Reachable     bool   `json:"reachable"`
	Compatible    bool   `json:"compatible"`
	StatusCode    int    `json:"status_code,omitempty"`
	ErrorClass    string `json:"error_class,omitempty"`
	Retryable     bool   `json:"retryable"`
	LatencyMS     int64  `json:"latency_ms"`
	Message       string `json:"message"`
	InputTokens   int64  `json:"input_tokens,omitempty"`
	OutputTokens  int64  `json:"output_tokens,omitempty"`
	UsageReported bool   `json:"usage_reported"`
}

type ProbeError struct {
	Result ProbeResult
	cause  error
}

func (e *ProbeError) Error() string {
	return e.Result.Message
}

func (e *ProbeError) Unwrap() error {
	return e.cause
}

type Prober struct {
	Client *http.Client
}

func (p Prober) Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	if request.Client == nil {
		request.Client = p.Client
	}
	return Probe(ctx, request)
}

func Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	result := ProbeResult{WireAPI: string(request.Profile.WireAPI)}
	if err := ValidateWireAPI(request.Profile.Agent, request.Profile.WireAPI); err != nil {
		return failedProbe(result, ProbeErrorInvalidConfig, false, err.Error())
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		return failedProbe(result, ProbeErrorInvalidConfig, false, "provider capability probe requires a model")
	}
	endpoint, payload, err := probeTarget(request.Profile, model)
	if err != nil {
		return failedProbe(result, ProbeErrorInvalidConfig, false, err.Error())
	}
	result.Endpoint = sanitizedEndpoint(endpoint)

	timeout := request.Timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return failedProbe(result, ProbeErrorInvalidConfig, false, fmt.Sprintf("build provider capability probe: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	request.Profile.ProviderType.injectProbeAuth(req.Header, request.Profile.Token)

	client := request.Client
	var ownedTransport *http.Transport
	if client == nil {
		transport := defaultProbeTransport()
		if disableEnvironmentProxy(request.Profile.Config) {
			transport.Proxy = nil
		}
		client = &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		ownedTransport = transport
	}
	if ownedTransport != nil {
		defer ownedTransport.CloseIdleConnections()
	}
	started := time.Now()
	resp, err := client.Do(req)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		if errors.Is(probeCtx.Err(), context.Canceled) {
			return failedProbeWithCause(result, ProbeErrorCanceled, false, "provider capability probe canceled", context.Canceled)
		}
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return failedProbeWithCause(result, ProbeErrorTimeout, true, fmt.Sprintf("provider capability probe timed out for %s", result.Endpoint), context.DeadlineExceeded)
		}
		return failedProbe(result, ProbeErrorUnavailable, true, fmt.Sprintf("provider capability probe failed for %s", result.Endpoint))
	}
	defer resp.Body.Close()
	result.Reachable = true
	result.StatusCode = resp.StatusCode
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return failedProbe(result, ProbeErrorInvalidResponse, false, fmt.Sprintf("read provider capability response from %s: %v", result.Endpoint, readErr))
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := validateProbeSuccess(request.Profile.WireAPI, body); err != nil {
			return failedProbe(result, ProbeErrorInvalidResponse, false, fmt.Sprintf("upstream returned an invalid %s response from %s", request.Profile.WireAPI, result.Endpoint))
		}
		result.Compatible = true
		result.InputTokens, result.OutputTokens, result.UsageReported = parseProbeUsage(request.Profile.WireAPI, body)
		result.Message = fmt.Sprintf("upstream supports %s", request.Profile.WireAPI)
		return result, nil
	}
	errorClass, retryable := classifyProbeFailure(resp.StatusCode, body)
	message := fmt.Sprintf("upstream returned HTTP %d for %s", resp.StatusCode, result.Endpoint)
	if errorClass == ProbeErrorUnsupportedProtocol {
		message = fmt.Sprintf("upstream does not provide the required %s API at %s", request.Profile.WireAPI, result.Endpoint)
	}
	return failedProbe(result, errorClass, retryable, message)
}

func parseProbeUsage(wireAPI WireAPI, body []byte) (int64, int64, bool) {
	var response struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, 0, false
	}
	if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
		return 0, 0, false
	}
	if response.Usage.InputTokens == 0 && response.Usage.OutputTokens == 0 {
		return 0, 0, false
	}
	return response.Usage.InputTokens, response.Usage.OutputTokens, true
}

func validateProbeSuccess(wireAPI WireAPI, body []byte) error {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	if len(response) == 0 {
		return fmt.Errorf("provider response must be a JSON object")
	}
	switch wireAPI {
	case WireAPIResponses:
		var id string
		if err := json.Unmarshal(response["id"], &id); err != nil || strings.TrimSpace(id) == "" {
			return fmt.Errorf("responses payload must include id")
		}
	case WireAPIAnthropicMessages:
		if _, ok := response["content"]; !ok {
			return fmt.Errorf("anthropic messages payload must include content")
		}
	default:
		return fmt.Errorf("unsupported wire_api %q", wireAPI)
	}
	return nil
}

func defaultProbeTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{ForceAttemptHTTP2: true}
}

func disableEnvironmentProxy(config map[string]string) bool {
	raw := strings.TrimSpace(config["upstream_no_proxy"])
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func probeTarget(profile Profile, model string) (*url.URL, []byte, error) {
	if profile.Upstream == nil {
		return nil, nil, fmt.Errorf("agent profile upstream is required")
	}
	endpoint := *profile.Upstream
	endpoint.Fragment = ""
	var payload any
	switch profile.WireAPI {
	case WireAPIResponses:
		endpoint.Path = joinProbePath(endpoint.Path, "responses")
		payload = map[string]any{"model": model, "input": "Reply with OK.", "max_output_tokens": 1}
	case WireAPIAnthropicMessages:
		endpoint.Path = joinProbePath(endpoint.Path, "v1/messages")
		payload = map[string]any{
			"model": model, "max_tokens": 1,
			"messages": []map[string]string{{"role": "user", "content": "Reply with OK."}},
		}
	default:
		return nil, nil, fmt.Errorf("unsupported wire_api %q", profile.WireAPI)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode provider capability probe: %w", err)
	}
	return &endpoint, encoded, nil
}

func joinProbePath(base string, suffix string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(suffix, "/")
}

func sanitizedEndpoint(endpoint *url.URL) string {
	clone := *endpoint
	clone.User = nil
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}

func (provider ProviderType) injectProbeAuth(header http.Header, token string) {
	switch provider {
	case ProviderAnthropic:
		header.Set("x-api-key", token)
		header.Set("anthropic-version", "2023-06-01")
	default:
		header.Set("Authorization", "Bearer "+token)
	}
}

func classifyProbeFailure(status int, body []byte) (string, bool) {
	normalized := strings.ToLower(string(body))
	if strings.Contains(normalized, "model") && (strings.Contains(normalized, "not found") || strings.Contains(normalized, "not_found") || strings.Contains(normalized, "does not exist")) {
		return ProbeErrorModelNotFound, false
	}
	switch status {
	case http.StatusUnauthorized:
		return ProbeErrorAuthentication, false
	case http.StatusForbidden:
		return ProbeErrorPermission, false
	case http.StatusNotFound:
		return ProbeErrorUnsupportedProtocol, false
	case http.StatusTooManyRequests:
		return ProbeErrorRateLimited, true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return ProbeErrorTimeout, true
	}
	if status >= 500 {
		return ProbeErrorUnavailable, true
	}
	return ProbeErrorInvalidResponse, false
}

func failedProbe(result ProbeResult, class string, retryable bool, message string) (ProbeResult, error) {
	return failedProbeWithCause(result, class, retryable, message, nil)
}

func failedProbeWithCause(result ProbeResult, class string, retryable bool, message string, cause error) (ProbeResult, error) {
	result.ErrorClass = class
	result.Retryable = retryable
	result.Message = message
	return result, &ProbeError{Result: result, cause: cause}
}
