package terminal

import (
	"context"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Resolver interface {
	AuthorizeAllocationAccess(context.Context, *gatewayv1.ResolveAllocationTerminalRequest) (*emptypb.Empty, error)
	ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type Manager struct {
	control Resolver
	nodes   nodekernel.ProcessStreamer
	options Options
	metrics *observability.Metrics
	obs     *sdkobs.Handle
}

type OpenOptions struct {
	Argv        []string
	Env         map[string]string
	User        string
	TTY         bool
	InitialCols uint32
	InitialRows uint32
}

func NewManager(control Resolver, nodes nodekernel.ProcessStreamer, options Options, metrics *observability.Metrics, obs *sdkobs.Handle) *Manager {
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 10 * time.Minute
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 2 * time.Hour
	}
	if options.AccessGrantRetryAttempts <= 0 {
		options.AccessGrantRetryAttempts = 3
	}
	if options.AccessGrantRetryDelay <= 0 {
		options.AccessGrantRetryDelay = 500 * time.Millisecond
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
	identity, ok := ctx.Value(credentialContextKey{}).(credentialIdentity)
	if !ok || identity.fingerprint == "" {
		return nil, status.Error(codes.Unauthenticated, "client credential is required")
	}
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
		AllocationID:          allocationID,
		TtlSeconds:            int64(m.options.MaxDuration / time.Second),
		CredentialFingerprint: identity.fingerprint,
		CredentialKind:        identity.kind,
		Purpose:               gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE,
	})
	if err != nil {
		op.SetErrorStatus("terminal resolve failed")
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrNodeID, resolved.GetNodeID()))
	return resolved, nil
}

func (m *Manager) Authorize(ctx context.Context, allocationID string) error {
	identity, ok := ctx.Value(credentialContextKey{}).(credentialIdentity)
	if !ok || identity.fingerprint == "" {
		return status.Error(codes.Unauthenticated, "client credential is required")
	}
	_, err := m.control.AuthorizeAllocationAccess(ctx, &gatewayv1.ResolveAllocationTerminalRequest{AllocationID: allocationID, CredentialFingerprint: identity.fingerprint, CredentialKind: identity.kind, Purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE})
	return err
}

func (m *Manager) OpenResolved(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse) (*Session, error) {
	return m.OpenResolvedWithOptions(ctx, resolved, OpenOptions{})
}

func (m *Manager) OpenResolvedWithOptions(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) (*Session, error) {
	grant := resolved.GetAccessGrant()
	if grant.GetExpiresAt() == nil || !time.Now().Before(grant.GetExpiresAt().AsTime()) {
		return nil, status.Error(codes.Unauthenticated, "allocation access grant has expired")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.GetExpiresAt().AsTime())
	stream, err := m.openProcess(ctx, resolved, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check, done := context.WithTimeout(ctx, 5*time.Second)
				err := m.Authorize(check, resolved.GetAllocationID())
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return &Session{stream: stream, cancel: cancel}, nil
}

func (m *Manager) openProcess(ctx context.Context, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts OpenOptions) (stream processStream, err error) {
	allocationID := strings.TrimSpace(resolved.GetAllocationID())
	attrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, allocationID),
		attribute.String(sdkobs.AttrNodeID, resolved.GetNodeID()),
	}
	ctx, op := m.obs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        observability.SpanTerminalProcessOpen,
		SpanAttrs:   attrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "process_open")},
		Counter:     observability.MetricTerminalProcessOpenTotal,
		Duration:    observability.MetricTerminalProcessOpenDuration,
	})
	defer func() {
		if err != nil {
			op.SetErrorStatus("process open failed")
		}
		op.End(err)
	}()
	current := resolved
	for attempt := 1; attempt <= m.options.AccessGrantRetryAttempts; attempt++ {
		backendCtx := nodekernel.WithAllocationAccessGrant(ctx, current.GetAccessGrant().GetPlaintextToken())
		stream, err = m.nodes.Process(backendCtx, current.GetNodeTarget(), current.GetNodeID())
		if err != nil {
			return nil, err
		}
		err = stream.Send(processOpenRequest(current, opts))
		if err == nil {
			var header metadata.MD
			header, err = stream.Header()
			if err == nil && !nodekernel.AllocationAccessGrantAccepted(header) {
				_, err = stream.Recv()
				if err == nil {
					err = status.Error(codes.FailedPrecondition, "node did not acknowledge allocation access grant before terminal output")
				}
			}
		}
		if err == nil {
			var ready *nodesandboxv1.ProcessResponse
			ready, err = stream.Recv()
			if err == nil && ready.GetReady() == nil {
				err = status.Error(codes.FailedPrecondition, "node process stream did not return ready after open")
			}
		}
		if err == nil {
			return stream, nil
		}
		_ = stream.CloseSend()
		if attempt == m.options.AccessGrantRetryAttempts || !nodekernel.IsAllocationAccessGrantRejected(err) {
			return nil, err
		}
		if m.metrics != nil {
			m.metrics.AccessGrantRetry("terminal")
		}
		if err := nodekernel.WaitAccessGrantRetry(ctx, attempt, m.options.AccessGrantRetryDelay); err != nil {
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
