package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Mode string
type Action string
type Protocol string
type Result string

const (
	ModeStrict  Mode = "strict"
	ModeDNSDeny Mode = "dns_deny"

	ActionPrepare Action = "prepare"
	ActionDelete  Action = "delete"
	ActionAllow   Action = "allow"
	ActionDeny    Action = "deny"

	ProtocolDNS   Protocol = "dns"
	ProtocolHTTP  Protocol = "http"
	ProtocolHTTPS Protocol = "https"
	ProtocolTCP   Protocol = "tcp"
	ProtocolUDP   Protocol = "udp"

	ResultOK      Result = "ok"
	ResultRefused Result = "refused"
	ResultError   Result = "error"
)

type Event struct {
	Mode         Mode
	Action       Action
	Protocol     Protocol
	Result       Result
	AllocationID string
	RuleCount    int64
	Latency      time.Duration
}

type Metrics struct {
	events    metric.Int64Counter
	latency   metric.Float64Histogram
	ruleCount metric.Int64Histogram
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	if meter == nil {
		meter = otel.Meter("github.com/cofy-x/axern/runtime/egressd")
	}
	events, err := meter.Int64Counter("axern.egressd_policy_events_total", metric.WithDescription("Egress policy lifecycle and enforcement outcomes"))
	if err != nil {
		return nil, err
	}
	latency, err := meter.Float64Histogram("axern.egressd_policy_latency_seconds", metric.WithDescription("Egress policy operation latency"), metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	ruleCount, err := meter.Int64Histogram("axern.egressd_policy_rule_count", metric.WithDescription("Normalized rule count per policy operation"))
	if err != nil {
		return nil, err
	}
	return &Metrics{events: events, latency: latency, ruleCount: ruleCount}, nil
}

// Record deliberately accepts no DNS name, HTTP Host, TLS SNI, remote address,
// or policy value. The fixed label set is safe for default telemetry export.
func (m *Metrics) Record(ctx context.Context, event Event) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("axern.mode", validMode(event.Mode)),
		attribute.String("axern.action", validAction(event.Action)),
		attribute.String("network.protocol.name", validProtocol(event.Protocol)),
		attribute.String("axern.result", validResult(event.Result)),
		attribute.String("axern.allocation_id", event.AllocationID),
	)
	m.events.Add(ctx, 1, attrs)
	m.latency.Record(ctx, event.Latency.Seconds(), attrs)
	m.ruleCount.Record(ctx, event.RuleCount, attrs)
}

func validMode(value Mode) string {
	switch value {
	case ModeStrict, ModeDNSDeny:
		return string(value)
	default:
		return "unknown"
	}
}

func validAction(value Action) string {
	switch value {
	case ActionPrepare, ActionDelete, ActionAllow, ActionDeny:
		return string(value)
	default:
		return "unknown"
	}
}

func validProtocol(value Protocol) string {
	switch value {
	case ProtocolDNS, ProtocolHTTP, ProtocolHTTPS, ProtocolTCP, ProtocolUDP:
		return string(value)
	default:
		return "unknown"
	}
}

func validResult(value Result) string {
	switch value {
	case ResultOK, ResultRefused, ResultError:
		return string(value)
	default:
		return "unknown"
	}
}
