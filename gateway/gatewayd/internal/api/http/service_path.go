package httpapi

import (
	"net/http"
	"strings"

	appservice "github.com/cofy-x/axern/gateway/gatewayd/internal/application/service"
)

type servicePath struct {
	appservice.RouteRef
	Upstream string
}

func parseServicePath(path string) (servicePath, bool) {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 5)
	if len(parts) < 4 || parts[0] != "svc" {
		return servicePath{}, false
	}
	out := servicePath{
		RouteRef: appservice.RouteRef{
			Namespace: strings.TrimSpace(parts[1]),
			ServiceID: strings.TrimSpace(parts[2]),
			PortRef:   strings.TrimSpace(parts[3]),
		},
		Upstream: "/",
	}
	if len(parts) == 5 && parts[4] != "" {
		out.Upstream = "/" + parts[4]
	}
	if out.Namespace == "" || out.ServiceID == "" || out.PortRef == "" {
		return servicePath{}, false
	}
	return out, true
}

func rewriteRequestPath(r *http.Request, upstream string) {
	if upstream == "" {
		upstream = "/"
	}
	r.URL.Path = upstream
	r.URL.RawPath = ""
}
