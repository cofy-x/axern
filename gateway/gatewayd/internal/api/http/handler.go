package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
)

type Handler struct {
	terminal *Terminal
}

func New(terminal *Terminal) *Handler {
	return &Handler{terminal: terminal}
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
	case strings.HasPrefix(r.URL.Path, "/terminal/allocation/") && h.terminal != nil:
		h.terminal.ServeHTTP(rec, r)
	default:
		logRecord.ErrorClass = "not_found"
		http.NotFound(rec, r)
	}
}
