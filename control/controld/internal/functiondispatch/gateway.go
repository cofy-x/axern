package functiondispatch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appfunction "github.com/cofy-x/axern/control/controld/internal/application/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	functionDispatchPath = "/function/invoke"
	defaultTimeout       = 30 * time.Second
	defaultMaxResponse   = 32 << 20

	gatewayErrorClassHeader = "X-Axern-Gateway-Error-Class"
	gatewayLeaseErrorClass  = "lease"
	leaseRetryAttempts      = 3
	leaseRetryBaseDelay     = 500 * time.Millisecond

	headerNamespace       = "X-Axern-Namespace"
	headerWorkerServiceID = "X-Axern-Worker-Service-Id"
	headerWorkerPort      = "X-Axern-Worker-Port"
	headerFunctionID      = "X-Axern-Function-Id"
	headerFunctionName    = "X-Axern-Function-Name"
	headerRevisionID      = "X-Axern-Function-Revision-Id"
	headerRequestID       = "X-Axern-Function-Request-Id"
	headerInvocationID    = "X-Axern-Function-Invocation-Id"

	functionWorkerPortRef = "function-http"
)

type GatewayConfig struct {
	URL              string
	Token            string
	Timeout          time.Duration
	MaxResponseBytes int64
	Client           *http.Client
}

type Gateway struct {
	baseURL          *url.URL
	token            string
	timeout          time.Duration
	maxResponseBytes int64
	client           *http.Client
}

func NewGateway(cfg GatewayConfig) (*Gateway, error) {
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		return nil, fmt.Errorf("function gateway url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse function gateway url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("function gateway url must include scheme and host")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultMaxResponse
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	return &Gateway{
		baseURL:          parsed,
		token:            strings.TrimSpace(cfg.Token),
		timeout:          cfg.Timeout,
		maxResponseBytes: cfg.MaxResponseBytes,
		client:           cfg.Client,
	}, nil
}

func (g *Gateway) InvokeFunctionWorker(ctx context.Context, req appfunction.FunctionInvokeDispatch) (*functionv1.FunctionResult, *functionv1.FunctionError, error) {
	if req.Deployment == nil || strings.TrimSpace(req.Deployment.GetWorkerServiceID()) == "" {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "function deployment has no worker service")
	}
	timeout := g.timeout
	if req.Timeout != nil && req.Timeout.CheckValid() == nil && req.Timeout.AsDuration() > 0 {
		timeout = req.Timeout.AsDuration()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload := req.Payload.GetData()
	for attempt := 1; attempt <= leaseRetryAttempts; attempt++ {
		result, fnErr, err, retry := g.invokeOnce(ctx, req, payload, attempt)
		if !retry {
			return result, fnErr, err
		}
		if err := sleepContext(ctx, time.Duration(attempt)*leaseRetryBaseDelay); err != nil {
			return nil, nil, grpcstatus.Errorf(codes.DeadlineExceeded, "function gateway dispatch retry deadline exceeded: %v", err)
		}
	}
	return nil, nil, grpcstatus.Error(codes.Unavailable, "function gateway dispatch failed: lease")
}

func (g *Gateway) invokeOnce(ctx context.Context, req appfunction.FunctionInvokeDispatch, payload []byte, attempt int) (*functionv1.FunctionResult, *functionv1.FunctionError, error, bool) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.invokeURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err, false
	}
	g.applyHeaders(httpReq, req)
	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, nil, grpcstatus.Errorf(codes.Unavailable, "function gateway dispatch failed: %v", err), false
	}
	defer resp.Body.Close()

	body, err := readCapped(resp.Body, g.maxResponseBytes)
	if err != nil {
		return nil, nil, err, false
	}
	if class := strings.TrimSpace(resp.Header.Get(gatewayErrorClassHeader)); class != "" {
		if class == gatewayLeaseErrorClass && attempt < leaseRetryAttempts {
			return nil, nil, nil, true
		}
		return nil, nil, grpcstatus.Errorf(gatewayErrorCode(class), "function gateway dispatch failed: %s", class), false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, workerHTTPError(resp.StatusCode, body), nil, false
	}
	return &functionv1.FunctionResult{
		ContentType: resp.Header.Get("Content-Type"),
		Data:        body,
	}, nil, nil, false
}

func gatewayErrorCode(class string) codes.Code {
	switch strings.TrimSpace(class) {
	case "body_too_large":
		return codes.ResourceExhausted
	case "timeout":
		return codes.DeadlineExceeded
	default:
		return codes.Unavailable
	}
}

func (g *Gateway) invokeURL() string {
	out := *g.baseURL
	out.Path = strings.TrimRight(out.Path, "/") + functionDispatchPath
	out.RawPath = ""
	out.RawQuery = ""
	out.Fragment = ""
	return out.String()
}

func (g *Gateway) applyHeaders(r *http.Request, req appfunction.FunctionInvokeDispatch) {
	if g.token != "" {
		r.Header.Set("Authorization", "Bearer "+g.token)
	}
	contentType := strings.TrimSpace(req.Payload.GetContentType())
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	r.Header.Set("Content-Type", contentType)
	r.Header.Set(headerNamespace, dispatchNamespace(req))
	r.Header.Set(headerWorkerServiceID, req.Deployment.GetWorkerServiceID())
	r.Header.Set(headerWorkerPort, functionWorkerPortRef)
	r.Header.Set(headerFunctionID, req.Function.GetID())
	r.Header.Set(headerFunctionName, req.Function.GetName())
	r.Header.Set(headerRevisionID, req.Revision.GetID())
	r.Header.Set(headerRequestID, req.Invocation.GetRequestID())
	r.Header.Set(headerInvocationID, req.Invocation.GetID())
}

func dispatchNamespace(req appfunction.FunctionInvokeDispatch) string {
	if namespace := strings.TrimSpace(req.Function.GetNamespace()); namespace != "" {
		return namespace
	}
	return strings.TrimSpace(req.Invocation.GetNamespace())
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	lr := &io.LimitedReader{R: r, N: limit + 1}
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, grpcstatus.Errorf(codes.ResourceExhausted, "function response exceeds %d bytes", limit)
	}
	return body, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func workerHTTPError(status int, body []byte) *functionv1.FunctionError {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return &functionv1.FunctionError{
		Code:    "worker_http_" + strconv.Itoa(status),
		Type:    "WorkerHTTPError",
		Message: message,
		Details: map[string]string{
			"http_status": strconv.Itoa(status),
		},
	}
}
