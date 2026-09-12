package observability

import (
	"context"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Metrics struct {
	activeTerminal sdkobs.UpDownCounter
	leaseRetries   sdkobs.Counter
	terminalEvents sdkobs.Counter
}

func NewMetrics(obs *sdkobs.Handle) *Metrics {
	if obs == nil {
		return &Metrics{}
	}
	return &Metrics{
		activeTerminal: obs.Int64UpDownCounter(MetricTerminalSessionsCurrent.Name, MetricTerminalSessionsCurrent.Description),
		leaseRetries:   obs.Int64Counter(MetricLeaseRetryTotal.Name, MetricLeaseRetryTotal.Description),
		terminalEvents: obs.Int64Counter(MetricTerminalEventTotal.Name, MetricTerminalEventTotal.Description),
	}
}

func (m *Metrics) IncActiveTerminal() func() {
	if m == nil {
		return func() {}
	}
	m.activeTerminal.Add(context.Background(), 1)
	return func() {
		m.activeTerminal.Add(context.Background(), -1)
	}
}

func (m *Metrics) LeaseRetry(routeType string) {
	if m == nil {
		return
	}
	m.leaseRetries.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrRouteType, normalizeLabel(routeType, "unknown")),
	)
}

func (m *Metrics) TerminalEvent(event string) {
	if m == nil {
		return
	}
	m.terminalEvents.Add(
		context.Background(),
		1,
		attribute.String(sdkobs.AttrEvent, normalizeLabel(event, "unknown")),
	)
}
