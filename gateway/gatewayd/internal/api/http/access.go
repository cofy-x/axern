package httpapi

import (
	"bufio"
	"net"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
)

func accessRecordForRequest(r *http.Request) observability.AccessLogRecord {
	record := observability.AccessLogRecord{
		Method:    r.Method,
		Path:      r.URL.Path,
		RouteType: "unknown",
	}
	switch {
	case r.URL.Path == "/healthz":
		record.RouteType = "health"
	case strings.HasPrefix(r.URL.Path, "/terminal/allocation/"):
		record.RouteType = "terminal"
		record.AllocationID = strings.Trim(strings.TrimPrefix(r.URL.Path, "/terminal/allocation/"), "/")
	}
	return record
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
