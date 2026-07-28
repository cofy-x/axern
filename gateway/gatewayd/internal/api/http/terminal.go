package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"github.com/gorilla/websocket"
)

type Terminal struct {
	auth     auth.DevToken
	manager  TerminalManager
	options  TerminalOptions
	metrics  *observability.Metrics
	upgrader websocket.Upgrader
}

type TerminalManager interface {
	Resolve(ctx context.Context, allocationID string) (*gatewayv1.ResolveAllocationTerminalResponse, error)
	OpenResolvedWithOptions(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts term.OpenOptions) (*term.Session, error)
}

func NewTerminal(token auth.DevToken, manager TerminalManager, options TerminalOptions, metrics *observability.Metrics) *Terminal {
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 10 * time.Minute
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 2 * time.Hour
	}
	if options.MaxMessageBytes <= 0 {
		options.MaxMessageBytes = 1 << 20
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = 10 * time.Second
	}
	return &Terminal{
		auth:    token,
		manager: manager,
		options: options,
		metrics: metrics,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (t *Terminal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !t.auth.Authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	allocationID := strings.TrimPrefix(r.URL.Path, "/terminal/allocation/")
	allocationID = strings.Trim(strings.TrimSpace(allocationID), "/")
	if allocationID == "" {
		http.NotFound(w, r)
		return
	}
	resolved, err := t.manager.Resolve(r.Context(), allocationID)
	if err != nil {
		http.Error(w, "terminal target unavailable", statusFromError(err))
		return
	}
	ws, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(r.Context(), t.options.MaxDuration)
	defer cancel()
	if t.metrics != nil {
		defer t.metrics.IncActiveTerminal()()
		t.metrics.TerminalEvent("open")
	}
	t.bridge(ctx, ws, resolved, terminalOpenOptionsFromRequest(r))
}

func (t *Terminal) nextReadDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(t.options.IdleTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}
