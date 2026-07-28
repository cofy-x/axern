package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/dashboard"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/serviceproxy"
	appservice "github.com/cofy-x/axern/gateway/gatewayd/internal/application/service"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	gatewayobs "github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStatusRecorderCapturesFirstStatus(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}

	rec.WriteHeader(http.StatusAccepted)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusAccepted)
	}
}

func TestDashboardDisabledReturnsNotFound(t *testing.T) {
	handler := New(nil, nil, nil, nil, auth.DevToken{Token: "secret"}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard?token=secret", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDashboardEnabledDoesNotAffectHealthz(t *testing.T) {
	dashboardHandler, err := dashboard.New(auth.DevToken{Token: "secret"}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(nil, nil, nil, dashboardHandler, auth.DevToken{Token: "secret"}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestFunctionInvokeDispatchesThroughServiceProxy(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoint: &gatewayv1.ServiceRouteEndpoint{
			AllocationID:  "alloc-1",
			NodeID:        "node-1",
			NodeTarget:    "127.0.0.1:25001",
			Attempt:       1,
			ContainerPort: 8080,
			Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
		},
		port: &gatewayv1.ServiceRoutePort{Name: "function-http", ContainerPort: 8080},
	}
	proxy := &fakeServiceProxy{status: http.StatusCreated, responseBody: "ok"}
	handler := New(routes, proxy, nil, nil, auth.DevToken{Token: "secret"}, false, nil)
	req := httptest.NewRequest(http.MethodPost, functionDispatchPath, strings.NewReader("payload"))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(functionHeaderNamespace, "default")
	req.Header.Set(functionHeaderWorkerServiceID, "svc-1")
	req.Header.Set("X-Axern-Function-Invocation-Id", "inv-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || rec.Body.String() != "ok" {
		t.Fatalf("response = code:%d body:%q", rec.Code, rec.Body.String())
	}
	if routes.got != (appservice.RouteRef{Namespace: "default", ServiceID: "svc-1", PortRef: "function-http"}) {
		t.Fatalf("route ref = %+v", routes.got)
	}
	if proxy.path != functionWorkerInvokePath {
		t.Fatalf("proxy path = %q, want %q", proxy.path, functionWorkerInvokePath)
	}
	if proxy.body != "payload" {
		t.Fatalf("proxy body = %q", proxy.body)
	}
	if proxy.header.Get("X-Axern-Function-Invocation-Id") != "inv-1" {
		t.Fatalf("invocation header not forwarded")
	}
}

func TestFunctionInvokeRejectsMissingRouteHeaders(t *testing.T) {
	t.Parallel()
	handler := New(&fakeRouteCache{}, &fakeServiceProxy{}, nil, nil, auth.DevToken{}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, functionDispatchPath, nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestFunctionInvokeRequiresConfiguredToken(t *testing.T) {
	t.Parallel()
	handler := New(&fakeRouteCache{}, &fakeServiceProxy{}, nil, nil, auth.DevToken{Token: "secret"}, false, nil)
	req := httptest.NewRequest(http.MethodPost, functionDispatchPath, nil)
	req.Header.Set(functionHeaderNamespace, "default")
	req.Header.Set(functionHeaderWorkerServiceID, "svc-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestServiceRetriesAlternateEndpointForRetryableFailure(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-a", NodeID: "node-a", ContainerPort: 8080},
			{AllocationID: "alloc-b", NodeID: "node-b", ContainerPort: 8080},
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	proxy := &fakeServiceProxy{
		endpointRetryAttempts: 2,
		attempts: []fakeProxyAttempt{
			{
				result: serviceproxy.Result{
					Status:            http.StatusGatewayTimeout,
					ErrorClass:        "timeout",
					Quarantine:        true,
					EndpointRetryable: true,
				},
				err: errors.New("connect allocation port: i/o timeout"),
			},
			{status: http.StatusOK, responseBody: "ok"},
		},
	}
	handler := New(routes, proxy, nil, nil, auth.DevToken{}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/svc/default/svc-1/8080/hello", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("response = code:%d body:%q", rec.Code, rec.Body.String())
	}
	if got := strings.Join(proxy.endpointIDs, ","); got != "alloc-a,alloc-b" {
		t.Fatalf("proxy endpoints = %q, want alloc-a,alloc-b", got)
	}
	if got := strings.Join(routes.quarantined, ","); got != "alloc-a" {
		t.Fatalf("quarantined endpoints = %q, want alloc-a", got)
	}
}

func TestServiceLeaseRejectionRefreshesRouteWithoutQuarantine(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-old", NodeID: "node-old", Attempt: 1, ContainerPort: 8080},
			{AllocationID: "alloc-new", NodeID: "node-new", Attempt: 2, ContainerPort: 8080},
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	proxy := &fakeServiceProxy{
		endpointRetryAttempts: 2,
		attempts: []fakeProxyAttempt{
			{
				result: serviceproxy.Result{
					Status:            http.StatusBadGateway,
					ErrorClass:        "lease",
					Invalidate:        true,
					LeaseRejected:     true,
					EndpointRetryable: true,
				},
				err: errors.New("stale lease"),
			},
			{status: http.StatusOK, responseBody: "ok"},
		},
	}
	handler := New(routes, proxy, nil, nil, auth.DevToken{}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/svc/default/svc-1/8080/hello", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("response = code:%d body:%q", rec.Code, rec.Body.String())
	}
	if got := strings.Join(proxy.endpointIDs, ","); got != "alloc-old,alloc-new" {
		t.Fatalf("proxy endpoints = %q, want alloc-old,alloc-new", got)
	}
	if routes.invalidated != 1 {
		t.Fatalf("route invalidations = %d, want 1", routes.invalidated)
	}
	if len(routes.quarantined) != 0 {
		t.Fatalf("quarantined endpoints = %#v, want none", routes.quarantined)
	}
	if proxy.waitCalls != 1 {
		t.Fatalf("lease retry waits = %d, want 1", proxy.waitCalls)
	}
}

func TestServiceLeaseRefreshReplaysBodyOnlyWithFreshAuthority(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-old", NodeID: "node-old", Attempt: 1, ContainerPort: 8080, Lease: &commonv1.ExecutionLease{PlaintextToken: "stale-token"}},
			{AllocationID: "alloc-new", NodeID: "node-new", Attempt: 2, ContainerPort: 8080, Lease: &commonv1.ExecutionLease{PlaintextToken: "fresh-token"}},
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	var tokens []string
	var forwardedBodies []string
	proxy := serviceproxy.New(handlerHTTPProxy(func(_ context.Context, spec nodekernel.HTTPProxySpec) (*http.Response, error) {
		tokens = append(tokens, spec.Token)
		if spec.Token == "stale-token" {
			return nil, status.Error(codes.Unauthenticated, "execution lease is invalid")
		}
		body, err := io.ReadAll(spec.Body)
		if err != nil {
			return nil, err
		}
		forwardedBodies = append(forwardedBodies, string(body))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}), serviceproxy.Options{
		UpstreamTimeout:       time.Second,
		MaxRequestBodyBytes:   1024,
		LeaseRetryBaseDelay:   time.Nanosecond,
		EndpointRetryAttempts: 2,
	}, nil, nil)
	handler := New(routes, proxy, nil, nil, auth.DevToken{}, false, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/svc/default/svc-1/8080/invoke", strings.NewReader("payload"))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("response = code:%d body:%q", rec.Code, rec.Body.String())
	}
	if got := strings.Join(tokens, ","); got != "stale-token,fresh-token" {
		t.Fatalf("lease tokens = %q, want stale-token,fresh-token", got)
	}
	if len(forwardedBodies) != 1 || forwardedBodies[0] != "payload" {
		t.Fatalf("forwarded bodies = %#v, want one payload", forwardedBodies)
	}
}

func TestServiceLeaseRetryStopsWhenBackoffIsCanceled(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-old", ContainerPort: 8080},
			{AllocationID: "alloc-new", ContainerPort: 8080},
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	proxy := &fakeServiceProxy{
		endpointRetryAttempts: 2,
		waitErr:               context.Canceled,
		attempts: []fakeProxyAttempt{{
			result: serviceproxy.Result{
				Status:            http.StatusBadGateway,
				ErrorClass:        "lease",
				Invalidate:        true,
				LeaseRejected:     true,
				EndpointRetryable: true,
			},
			err: status.Error(codes.Unauthenticated, "stale lease"),
		}},
	}
	handler := New(routes, proxy, nil, nil, auth.DevToken{}, false, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/svc/default/svc-1/8080/hello", nil))

	if got := strings.Join(proxy.endpointIDs, ","); got != "alloc-old" {
		t.Fatalf("proxy endpoints = %q, want only alloc-old", got)
	}
	if proxy.waitCalls != 1 {
		t.Fatalf("lease retry waits = %d, want 1", proxy.waitCalls)
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
}

func TestServiceDoesNotRetryUnsafeMethodForRetryableFailure(t *testing.T) {
	t.Parallel()
	routes := &fakeRouteCache{
		endpoints: []*gatewayv1.ServiceRouteEndpoint{
			{AllocationID: "alloc-a", NodeID: "node-a", ContainerPort: 8080},
			{AllocationID: "alloc-b", NodeID: "node-b", ContainerPort: 8080},
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	proxy := &fakeServiceProxy{
		endpointRetryAttempts: 2,
		attempts: []fakeProxyAttempt{
			{
				result: serviceproxy.Result{
					Status:            http.StatusGatewayTimeout,
					ErrorClass:        "timeout",
					Quarantine:        true,
					EndpointRetryable: true,
				},
				err: errors.New("connect allocation port: i/o timeout"),
			},
			{status: http.StatusOK, responseBody: "ok"},
		},
	}
	handler := New(routes, proxy, nil, nil, auth.DevToken{}, false, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/svc/default/svc-1/8080/hello", strings.NewReader("payload")))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}
	if got := strings.Join(proxy.endpointIDs, ","); got != "alloc-a" {
		t.Fatalf("proxy endpoints = %q, want alloc-a", got)
	}
	if got := strings.Join(routes.quarantined, ","); got != "alloc-a" {
		t.Fatalf("quarantined endpoints = %q, want alloc-a", got)
	}
}

func TestServiceProxyTotalIncludesRouteResolution(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	obs, err := sdkobs.Init(context.Background(), sdkobs.Config{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	metrics := gatewayobs.NewMetrics(obs)
	delay := 20 * time.Millisecond
	routes := &fakeRouteCache{
		delay: delay,
		endpoint: &gatewayv1.ServiceRouteEndpoint{
			AllocationID:  "alloc-1",
			NodeID:        "node-1",
			ContainerPort: 8080,
		},
		port: &gatewayv1.ServiceRoutePort{Name: "8080", ContainerPort: 8080},
	}
	handler := New(routes, &fakeServiceProxy{status: http.StatusOK, responseBody: "ok"}, nil, nil, auth.DevToken{}, false, metrics)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/svc/default/svc-1/8080/hello", nil))

	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	found := false
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != gatewayobs.MetricServiceProxyStageDuration.Name {
				continue
			}
			histogram, ok := metric.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric data type = %T, want float64 histogram", metric.Data)
			}
			for _, point := range histogram.DataPoints {
				stage, ok := point.Attributes.Value(attribute.Key(sdkobs.AttrStage))
				if !ok || stage.AsString() != "total" {
					continue
				}
				found = true
				if point.Sum < delay.Seconds() {
					t.Fatalf("total proxy duration = %fs, want at least route delay %fs", point.Sum, delay.Seconds())
				}
			}
		}
	}
	if !found {
		t.Fatal("total service proxy stage metric not found")
	}
}

type fakeRouteCache struct {
	got         appservice.RouteRef
	endpoint    *gatewayv1.ServiceRouteEndpoint
	endpoints   []*gatewayv1.ServiceRouteEndpoint
	port        *gatewayv1.ServiceRoutePort
	err         error
	delay       time.Duration
	calls       int
	quarantined []string
	invalidated int
}

func (f *fakeRouteCache) Resolve(_ context.Context, ref appservice.RouteRef) (*gatewayv1.ServiceRouteEndpoint, *gatewayv1.ServiceRoutePort, error) {
	f.got = ref
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.err != nil {
		return nil, nil, f.err
	}
	if len(f.endpoints) > 0 {
		ep := f.endpoints[f.calls%len(f.endpoints)]
		f.calls++
		return ep, f.port, nil
	}
	return f.endpoint, f.port, nil
}

func (f *fakeRouteCache) ReportEndpointResult(appservice.RouteRef, *gatewayv1.ServiceRouteEndpoint, time.Duration, bool) {
}

func (f *fakeRouteCache) Invalidate(appservice.RouteRef) { f.invalidated++ }

func (f *fakeRouteCache) QuarantineEndpoint(_ appservice.RouteRef, ep *gatewayv1.ServiceRouteEndpoint, _ string) {
	f.quarantined = append(f.quarantined, ep.GetAllocationID())
}

type fakeProxyAttempt struct {
	status       int
	responseBody string
	result       serviceproxy.Result
	err          error
}

type handlerHTTPProxy func(context.Context, nodekernel.HTTPProxySpec) (*http.Response, error)

func (f handlerHTTPProxy) ProxyHTTP(ctx context.Context, spec nodekernel.HTTPProxySpec) (*http.Response, error) {
	return f(ctx, spec)
}

type fakeServiceProxy struct {
	status                int
	responseBody          string
	body                  string
	path                  string
	header                http.Header
	endpointRetryAttempts int
	attempts              []fakeProxyAttempt
	endpointIDs           []string
	waitCalls             int
	waitErr               error
}

func (f *fakeServiceProxy) RoundTrip(r *http.Request, ep *gatewayv1.ServiceRouteEndpoint) (*http.Response, serviceproxy.Result, error) {
	f.path = r.URL.Path
	f.header = r.Header.Clone()
	f.endpointIDs = append(f.endpointIDs, ep.GetAllocationID())
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		f.body = string(body)
	}
	if len(f.attempts) > 0 {
		attempt := f.attempts[0]
		f.attempts = f.attempts[1:]
		if attempt.err != nil {
			return nil, attempt.result, attempt.err
		}
		status := attempt.status
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(attempt.responseBody)),
		}, serviceproxy.Result{Status: status}, nil
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(f.responseBody)),
	}, serviceproxy.Result{Status: status}, nil
}

func (f *fakeServiceProxy) WriteResponse(w http.ResponseWriter, resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (f *fakeServiceProxy) WriteError(w http.ResponseWriter, result serviceproxy.Result) {
	w.Header().Set(serviceproxy.GatewayErrorClassHeader, result.ErrorClass)
	http.Error(w, http.StatusText(result.Status), result.Status)
}

func (f *fakeServiceProxy) EndpointRetryAttempts() int {
	if f.endpointRetryAttempts <= 0 {
		return 1
	}
	return f.endpointRetryAttempts
}

func (f *fakeServiceProxy) WaitLeaseRetry(ctx context.Context, _ int) error {
	f.waitCalls++
	if f.waitErr != nil {
		return f.waitErr
	}
	return ctx.Err()
}
