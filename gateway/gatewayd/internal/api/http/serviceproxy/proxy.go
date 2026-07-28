package serviceproxy

import (
	"context"
	"net/http"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

type Proxy struct {
	proxy   nodekernel.HTTPProxyer
	options Options
	metrics *observability.Metrics
	obs     *sdkobs.Handle
}

type Options struct {
	UpstreamTimeout       time.Duration
	MaxRequestBodyBytes   int64
	LeaseRetryBaseDelay   time.Duration
	EndpointRetryAttempts int
}

type Result struct {
	Status            int
	ErrorClass        string
	Invalidate        bool
	Quarantine        bool
	LeaseRejected     bool
	EndpointRetryable bool
}

const GatewayErrorClassHeader = "X-Axern-Gateway-Error-Class"

func New(proxy nodekernel.HTTPProxyer, options Options, metrics *observability.Metrics, obs *sdkobs.Handle) *Proxy {
	if options.UpstreamTimeout <= 0 {
		options.UpstreamTimeout = 30 * time.Second
	}
	if options.MaxRequestBodyBytes <= 0 {
		options.MaxRequestBodyBytes = 32 << 20
	}
	if options.LeaseRetryBaseDelay <= 0 {
		options.LeaseRetryBaseDelay = 500 * time.Millisecond
	}
	if options.EndpointRetryAttempts <= 0 {
		options.EndpointRetryAttempts = 4
	}
	return &Proxy{proxy: proxy, options: options, metrics: metrics, obs: obs}
}

func (p *Proxy) EndpointRetryAttempts() int {
	return p.options.EndpointRetryAttempts
}

func (p *Proxy) WaitLeaseRetry(ctx context.Context, failedAttempt int) error {
	return nodekernel.WaitLeaseRetry(ctx, failedAttempt, p.options.LeaseRetryBaseDelay)
}

func (p *Proxy) RoundTrip(r *http.Request, ep *gatewayv1.ServiceRouteEndpoint) (*http.Response, Result, error) {
	status := 0
	ctx, op := p.obs.StartOperation(r.Context(), sdkobs.OperationConfig{
		Name: observability.SpanServiceProxy,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrAllocationID, ep.GetAllocationID()),
			attribute.String(sdkobs.AttrNodeID, ep.GetNodeID()),
			attribute.Int(sdkobs.AttrContainerPort, int(ep.GetContainerPort())),
		},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "service_proxy")},
		Counter:     observability.MetricServiceProxyRequests,
		Duration:    observability.MetricServiceProxyDuration,
	})
	var opErr error
	defer func() {
		op.SetAttributes(attribute.Int(sdkobs.AttrHTTPStatusCode, status))
		op.AddMetricAttributes(attribute.Int(sdkobs.AttrHTTPStatusCode, status))
		op.End(opErr)
	}()
	if p.options.MaxRequestBodyBytes > 0 && r.ContentLength > p.options.MaxRequestBodyBytes {
		status = http.StatusRequestEntityTooLarge
		op.SetResult("body_too_large")
		op.SetErrorClass("body_too_large")
		op.SetErrorStatus("request body too large")
		err := errRequestBodyTooLarge{}
		opErr = err
		closeRequestBody(r.Body)
		return nil, Result{Status: http.StatusRequestEntityTooLarge, ErrorClass: "body_too_large"}, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.options.UpstreamTimeout)
	req := r.WithContext(ctx)
	if p.options.MaxRequestBodyBytes > 0 && req.Body != nil && req.Body != http.NoBody {
		req.Body = http.MaxBytesReader(nil, req.Body, p.options.MaxRequestBodyBytes)
	}
	attemptReq, sharedBody, err := cloneUpstreamRequest(req)
	if err != nil {
		cancel()
		closeRequestBody(req.Body)
		status = http.StatusBadGateway
		op.SetResult("upstream")
		op.SetErrorClass("upstream")
		op.SetErrorStatus("upstream")
		opErr = err
		return nil, Result{Status: status, ErrorClass: "upstream"}, err
	}
	resp, err := p.roundTrip(attemptReq, ep)
	if err != nil {
		cancel()
		result := classifyProxyError(err)
		if result.LeaseRejected {
			closeRejectedLeaseAttempt(attemptReq.Body, sharedBody)
		} else {
			closeAttemptRequestBodies(req.Body, attemptReq.Body, sharedBody)
		}
		status = result.Status
		if p.metrics != nil {
			p.metrics.UpstreamFailure(result.ErrorClass)
		}
		op.SetResult(result.ErrorClass)
		op.SetErrorClass(result.ErrorClass)
		op.SetErrorStatus(result.ErrorClass)
		opErr = err
		logrus.WithError(err).WithFields(logrus.Fields{
			"allocation_id": ep.GetAllocationID(),
			"node_id":       ep.GetNodeID(),
			"node_target":   ep.GetNodeTarget(),
			"port":          ep.GetContainerPort(),
			"error_class":   result.ErrorClass,
		}).Warn("gateway service proxy upstream request failed")
		return nil, result, err
	}
	status = resp.StatusCode
	resp.Body = cancelOnCloseReadCloser{
		ReadCloser:    resp.Body,
		cancel:        cancel,
		requestBodies: responseRequestBodies(req.Body, attemptReq.Body, sharedBody),
	}
	return resp, Result{Status: resp.StatusCode}, nil
}

func (p *Proxy) WriteResponse(w http.ResponseWriter, resp *http.Response) {
	start := time.Now()
	result := "ok"
	errorClass := ""
	if err := copyUpstreamResponse(w, resp); err != nil {
		result = "error"
		errorClass = "response_copy"
		logrus.WithError(err).WithField("status", resp.StatusCode).Warn("gateway service proxy response copy failed")
	}
	p.observeStage("response_copy", result, errorClass, resp.Request.Method, time.Since(start))
}

func (p *Proxy) WriteError(w http.ResponseWriter, result Result) {
	status := result.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	if result.ErrorClass != "" {
		w.Header().Set(GatewayErrorClassHeader, result.ErrorClass)
	}
	http.Error(w, http.StatusText(status), status)
}

func (p *Proxy) roundTrip(r *http.Request, ep *gatewayv1.ServiceRouteEndpoint) (*http.Response, error) {
	start := time.Now()
	resp, err := p.proxy.ProxyHTTP(r.Context(), nodekernel.HTTPProxySpec{
		NodeTarget:    ep.GetNodeTarget(),
		AllocationID:  ep.GetAllocationID(),
		Attempt:       ep.GetAttempt(),
		Token:         ep.GetLease().GetPlaintextToken(),
		Port:          ep.GetContainerPort(),
		Method:        r.Method,
		Path:          r.URL.EscapedPath(),
		Query:         r.URL.RawQuery,
		Header:        upstreamHeaders(r.Header),
		Body:          r.Body,
		HasBody:       requestHasBody(r),
		ContentLength: r.ContentLength,
		Timeout:       p.options.UpstreamTimeout,
	})
	result := "ok"
	errorClass := ""
	if err != nil {
		result = "error"
		errorClass = classifyProxyError(err).ErrorClass
	}
	p.observeStage("node_proxy_round_trip", result, errorClass, r.Method, time.Since(start))
	if resp != nil {
		resp.Request = r
	}
	return resp, err
}

func requestHasBody(r *http.Request) bool {
	return r.Body != nil && r.Body != http.NoBody && (r.ContentLength != 0 || len(r.TransferEncoding) > 0)
}

func (p *Proxy) observeStage(stage, result, errorClass, method string, duration time.Duration) {
	if p.metrics != nil {
		p.metrics.ObserveServiceProxyStage(stage, result, errorClass, method, duration)
	}
}

type ioCloser interface {
	Close() error
}

func closeRequestBody(body ioCloser) {
	if body == nil || body == http.NoBody {
		return
	}
	_ = body.Close()
}

func closeAttemptRequestBodies(rootBody, attemptBody ioCloser, sharedBody bool) {
	closeRequestBody(attemptBody)
	if !sharedBody {
		closeRequestBody(rootBody)
	}
}

func closeRejectedLeaseAttempt(attemptBody ioCloser, sharedBody bool) {
	if !sharedBody {
		closeRequestBody(attemptBody)
	}
}

func responseRequestBodies(rootBody, attemptBody ioCloser, sharedBody bool) []ioCloser {
	if sharedBody {
		return []ioCloser{attemptBody}
	}
	return []ioCloser{attemptBody, rootBody}
}

func closeRequestBodies(bodies []ioCloser) error {
	var firstErr error
	for _, body := range bodies {
		if body == nil || body == http.NoBody {
			continue
		}
		if err := body.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
