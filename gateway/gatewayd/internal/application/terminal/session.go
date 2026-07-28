package terminal

import (
	"context"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Resolver interface {
	ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type Manager struct {
	control Resolver
	nodes   nodekernel.ExecStreamer
	options Options
	metrics *observability.Metrics
	obs     *sdkobs.Handle
}

type OpenOptions struct {
	Argv []string
	Env  map[string]string
	User string
	TTY  bool
}

func NewManager(control Resolver, nodes nodekernel.ExecStreamer, options Options, metrics *observability.Metrics, obs *sdkobs.Handle) *Manager {
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 10 * time.Minute
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 2 * time.Hour
	}
	if options.LeaseRetryAttempts <= 0 {
		options.LeaseRetryAttempts = 3
	}
	if options.LeaseRetryDelay <= 0 {
		options.LeaseRetryDelay = 500 * time.Millisecond
	}
	return &Manager{control: control, nodes: nodes, options: options, metrics: metrics, obs: obs}
}

func (m *Manager) Options() Options {
	if m == nil {
		return Options{}
	}
	return m.options
}

func (m *Manager) Open(ctx context.Context, allocationID string) (*Session, error) {
	resolved, err := m.Resolve(ctx, allocationID)
	if err != nil {
		return nil, err
	}
	return m.OpenResolved(ctx, resolved)
}

func (m *Manager) OpenWithOptions(ctx context.Context, allocationID string, opts OpenOptions) (*Session, error) {
	resolved, err := m.Resolve(ctx, allocationID)
	if err != nil {
		return nil, err
	}
	return m.OpenResolvedWithOptions(ctx, resolved, opts)
}

func (m *Manager) Resolve(ctx context.Context, allocationID string) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	allocationID = strings.TrimSpace(allocationID)
	ctx, op := m.obs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        observability.SpanTerminalResolve,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrAllocationID, allocationID)},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "resolve")},
		Counter:     observability.MetricTerminalResolveTotal,
		Duration:    observability.MetricTerminalResolveDuration,
	})
	var err error
	defer func() { op.End(err) }()
	resolved, err := m.control.ResolveAllocationTerminal(ctx, &gatewayv1.ResolveAllocationTerminalRequest{
		AllocationID: allocationID,
		TtlSeconds:   300,
	})
	if err != nil {
		op.SetErrorStatus("terminal resolve failed")
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrNodeID, resolved.GetNodeID()))
	return resolved, nil
}

func (m *Manager) OpenResolved(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse) (*Session, error) {
	return m.OpenResolvedWithOptions(ctx, resolved, OpenOptions{})
}

func (m *Manager) OpenResolvedWithOptions(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) (*Session, error) {
	stream, err := m.openExecStream(ctx, resolved, opts)
	if err != nil {
		return nil, err
	}
	return &Session{stream: stream}, nil
}

func (m *Manager) openExecStream(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) (stream execStream, err error) {
	allocationID := strings.TrimSpace(resolved.GetAllocationID())
	attrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, allocationID),
		attribute.String(sdkobs.AttrNodeID, resolved.GetNodeID()),
	}
	ctx, op := m.obs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        observability.SpanTerminalExecStreamOpen,
		SpanAttrs:   attrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "exec_stream_open")},
		Counter:     observability.MetricTerminalExecStreamOpenTotal,
		Duration:    observability.MetricTerminalExecStreamOpenDuration,
	})
	defer func() {
		if err != nil {
			op.SetErrorStatus("exec stream open failed")
		}
		op.End(err)
	}()
	current := resolved
	for attempt := 1; attempt <= m.options.LeaseRetryAttempts; attempt++ {
		stream, err = m.nodes.ExecStream(ctx, current.GetNodeTarget())
		if err != nil {
			return nil, err
		}
		err = stream.Send(execStreamOpenRequest(current, opts))
		if err == nil {
			var header metadata.MD
			header, err = stream.Header()
			if err == nil && !nodekernel.ExecutionLeaseAccepted(header) {
				_, err = stream.Recv()
				if err == nil {
					err = status.Error(codes.FailedPrecondition, "node did not acknowledge execution lease before terminal output")
				}
			}
		}
		if err == nil {
			return stream, nil
		}
		_ = stream.CloseSend()
		if attempt == m.options.LeaseRetryAttempts || !nodekernel.IsExecutionLeaseRejected(err) {
			return nil, err
		}
		if m.metrics != nil {
			m.metrics.LeaseRetry("terminal")
		}
		if err := nodekernel.WaitLeaseRetry(ctx, attempt, m.options.LeaseRetryDelay); err != nil {
			return nil, err
		}
		current, err = m.Resolve(ctx, allocationID)
		if err != nil {
			return nil, err
		}
		op.SetAttributes(attribute.String(sdkobs.AttrNodeID, current.GetNodeID()))
	}
	return nil, err
}
