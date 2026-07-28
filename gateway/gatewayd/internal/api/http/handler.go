package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/dashboard"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/serviceproxy"
	appservice "github.com/cofy-x/axern/gateway/gatewayd/internal/application/service"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"github.com/sirupsen/logrus"
)

type RouteCache interface {
	Resolve(rctx context.Context, p appservice.RouteRef) (*gatewayv1.ServiceRouteEndpoint, *gatewayv1.ServiceRoutePort, error)
	ReportEndpointResult(p appservice.RouteRef, ep *gatewayv1.ServiceRouteEndpoint, latency time.Duration, ok bool)
	Invalidate(p appservice.RouteRef)
	QuarantineEndpoint(p appservice.RouteRef, ep *gatewayv1.ServiceRouteEndpoint, reason string)
}

type ServiceProxy interface {
	RoundTrip(r *http.Request, ep *gatewayv1.ServiceRouteEndpoint) (*http.Response, serviceproxy.Result, error)
	WriteResponse(w http.ResponseWriter, resp *http.Response)
	WriteError(w http.ResponseWriter, result serviceproxy.Result)
	EndpointRetryAttempts() int
	WaitLeaseRetry(ctx context.Context, failedAttempt int) error
}

type Handler struct {
	routes          RouteCache
	proxy           ServiceProxy
	terminal        *Terminal
	dashboard       *dashboard.Handler
	auth            auth.DevToken
	requireHTTPAuth bool
	metrics         *observability.Metrics
}

func New(routes RouteCache, proxy ServiceProxy, terminal *Terminal, dashboardHandler *dashboard.Handler, token auth.DevToken, requireHTTPAuth bool, metrics *observability.Metrics) *Handler {
	return &Handler{routes: routes, proxy: proxy, terminal: terminal, dashboard: dashboardHandler, auth: token, requireHTTPAuth: requireHTTPAuth, metrics: metrics}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w}
	logRecord := accessRecordForRequest(r)
	defer func() {
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		logRecord.Status = status
		logRecord.Duration = time.Since(start)
		observability.LogAccess(logRecord)
	}()
	switch {
	case r.URL.Path == "/healthz":
		writeJSON(rec, http.StatusOK, map[string]string{"status": "ok"})
	case h.dashboard != nil && h.dashboard.Handles(r.URL.Path):
		h.dashboard.ServeHTTP(rec, r)
	case r.URL.Path == functionDispatchPath:
		h.serveFunctionInvoke(rec, r, &logRecord)
	case strings.HasPrefix(r.URL.Path, "/svc/"):
		h.serveService(rec, r, &logRecord)
	case strings.HasPrefix(r.URL.Path, "/terminal/allocation/"):
		h.terminal.ServeHTTP(rec, r)
	default:
		logRecord.ErrorClass = "not_found"
		http.NotFound(rec, r)
	}
}

func (h *Handler) serveService(w http.ResponseWriter, r *http.Request, logRecord *observability.AccessLogRecord) {
	if h.requireHTTPAuth && !h.auth.Authorized(r) {
		logRecord.ErrorClass = "unauthorized"
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	parsed, ok := parseServicePath(r.URL.Path)
	if !ok {
		logRecord.ErrorClass = "not_found"
		http.NotFound(w, r)
		return
	}
	logRecord.Namespace = parsed.Namespace
	logRecord.ServiceID = parsed.ServiceID
	logRecord.Port = parsed.PortRef
	rewriteRequestPath(r, parsed.Upstream)
	h.serveProxiedRoute(w, r, parsed.RouteRef, logRecord)
}

func (h *Handler) serveProxiedRoute(w http.ResponseWriter, r *http.Request, ref appservice.RouteRef, logRecord *observability.AccessLogRecord) {
	start := time.Now()
	totalResult := "error"
	totalErrorClass := "internal"
	defer func() {
		if h.metrics != nil {
			h.metrics.ObserveServiceProxyStage("total", totalResult, totalErrorClass, r.Method, time.Since(start))
		}
	}()
	if h.metrics != nil {
		defer h.metrics.IncActiveHTTP()()
	}
	attempts := h.proxy.EndpointRetryAttempts()
	if attempts <= 0 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		ep, _, err := h.routes.Resolve(r.Context(), ref)
		if err != nil {
			status := statusFromError(err)
			logRecord.ErrorClass = errorClassFromStatus(status)
			totalErrorClass = logRecord.ErrorClass
			w.Header().Set(serviceproxy.GatewayErrorClassHeader, logRecord.ErrorClass)
			http.Error(w, http.StatusText(status), status)
			return
		}
		logRecord.AllocationID = ep.GetAllocationID()
		logRecord.NodeID = ep.GetNodeID()
		attemptStart := time.Now()
		resp, result, err := h.proxy.RoundTrip(r, ep)
		logRecord.ErrorClass = result.ErrorClass
		if err == nil {
			h.proxy.WriteResponse(w, resp)
			h.routes.ReportEndpointResult(ref, ep, time.Since(attemptStart), true)
			totalResult = "ok"
			totalErrorClass = ""
			return
		}
		h.routes.ReportEndpointResult(ref, ep, time.Since(attemptStart), false)
		if result.Quarantine {
			h.routes.QuarantineEndpoint(ref, ep, result.ErrorClass)
		}
		if result.Invalidate {
			h.routes.Invalidate(ref)
		}
		if !shouldRetryEndpoint(r, result, attempt, attempts) {
			totalErrorClass = result.ErrorClass
			if result.LeaseRejected {
				closeRequestBody(r.Body)
			}
			h.proxy.WriteError(w, result)
			return
		}
		if result.LeaseRejected {
			if err := h.proxy.WaitLeaseRetry(r.Context(), attempt); err != nil {
				totalErrorClass = "timeout"
				closeRequestBody(r.Body)
				h.proxy.WriteError(w, serviceproxy.Result{Status: http.StatusGatewayTimeout, ErrorClass: "timeout"})
				return
			}
			if h.metrics != nil {
				h.metrics.LeaseRetry("service")
			}
		}
		logrus.WithError(err).WithFields(logrus.Fields{
			"namespace":     ref.Namespace,
			"service_id":    ref.ServiceID,
			"port_ref":      ref.PortRef,
			"allocation_id": ep.GetAllocationID(),
			"node_id":       ep.GetNodeID(),
			"error_class":   result.ErrorClass,
			"attempt":       attempt,
			"max_attempts":  attempts,
		}).Info("gateway service proxy retrying request")
	}
}

func shouldRetryEndpoint(r *http.Request, result serviceproxy.Result, attempt, attempts int) bool {
	if attempt >= attempts || !result.EndpointRetryable {
		return false
	}
	if result.LeaseRejected {
		return true
	}
	return serviceproxy.RequestEndpointRetryable(r)
}

func closeRequestBody(body io.ReadCloser) {
	if body != nil {
		_ = body.Close()
	}
}
