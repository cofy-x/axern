package httpapi

import (
	"net/http"
	"strings"

	appservice "github.com/cofy-x/axern/gateway/gatewayd/internal/application/service"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
)

const (
	functionDispatchPath = "/function/invoke"

	functionHeaderNamespace       = "X-Axern-Namespace"
	functionHeaderWorkerServiceID = "X-Axern-Worker-Service-Id"
	functionHeaderWorkerPort      = "X-Axern-Worker-Port"

	defaultFunctionWorkerPortRef = "function-http"
	functionWorkerInvokePath     = "/invoke"
)

func (h *Handler) serveFunctionInvoke(w http.ResponseWriter, r *http.Request, logRecord *observability.AccessLogRecord) {
	if r.Method != http.MethodPost {
		logRecord.ErrorClass = "method_not_allowed"
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.TrimSpace(h.auth.Token) != "" && !h.auth.Authorized(r) {
		logRecord.ErrorClass = "unauthorized"
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ref, ok := functionRouteRef(r)
	if !ok {
		logRecord.ErrorClass = "invalid"
		http.Error(w, "missing function dispatch route headers", http.StatusBadRequest)
		return
	}
	logRecord.Namespace = ref.Namespace
	logRecord.ServiceID = ref.ServiceID
	logRecord.Port = ref.PortRef

	rewriteRequestPath(r, functionWorkerInvokePath)
	h.serveProxiedRoute(w, r, ref, logRecord)
}

func functionRouteRef(r *http.Request) (appservice.RouteRef, bool) {
	ref := appservice.RouteRef{
		Namespace: strings.TrimSpace(r.Header.Get(functionHeaderNamespace)),
		ServiceID: strings.TrimSpace(r.Header.Get(functionHeaderWorkerServiceID)),
		PortRef:   strings.TrimSpace(r.Header.Get(functionHeaderWorkerPort)),
	}
	if ref.PortRef == "" {
		ref.PortRef = defaultFunctionWorkerPortRef
	}
	if ref.Namespace == "" || ref.ServiceID == "" {
		return appservice.RouteRef{}, false
	}
	return ref, true
}
