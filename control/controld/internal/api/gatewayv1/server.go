package gatewayv1

import (
	"context"
	"errors"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Resolver interface {
	ResolveServiceRoute(ctx context.Context, req *gatewayv1.ResolveServiceRouteRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveServiceRouteResponse, error)
	ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error)
	ResolveServiceReplicaTargets(context.Context, string) (*gatewayv1.ResolveServiceReplicaTargetsResponse, error)
}

type TunnelResolver interface {
	Get(context.Context, string, time.Time) (*tunnelv1.TunnelSession, error)
}

type Dependencies struct {
	Now        func() time.Time
	Resolver   Resolver
	DefaultTTL time.Duration
	Access     AccessAuthorizer
	Tunnels    TunnelResolver
}

func (s *Server) ResolveTunnelRelayTarget(ctx context.Context, req *gatewayv1.ResolveTunnelRelayTargetRequest) (*gatewayv1.ResolveTunnelRelayTargetResponse, error) {
	if s.deps.Tunnels == nil {
		return nil, status.Error(codes.Unavailable, "tunnel resolver is not configured")
	}
	session, err := s.deps.Tunnels.Get(ctx, req.GetSessionID(), s.now())
	if err != nil {
		return nil, err
	}
	if session.GetNodeEdgeTarget() == "" {
		return nil, status.Error(codes.Unavailable, "tunnel relay target is not ready")
	}
	return &gatewayv1.ResolveTunnelRelayTargetResponse{NodeEdgeTarget: session.GetNodeEdgeTarget()}, nil
}

func (s *Server) ResolveServiceReplicaTargets(ctx context.Context, req *gatewayv1.ResolveServiceReplicaTargetsRequest) (*gatewayv1.ResolveServiceReplicaTargetsResponse, error) {
	return s.deps.Resolver.ResolveServiceReplicaTargets(ctx, req.GetServiceID())
}

type AccessAuthorizer interface {
	AuthorizeFingerprintResource(context.Context, string, string, accesskernel.Action, string, string) error
}

type Server struct {
	gatewayv1.UnimplementedGatewayControlServer
	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}

func (s *Server) ResolveServiceRoute(ctx context.Context, req *gatewayv1.ResolveServiceRouteRequest) (*gatewayv1.ResolveServiceRouteResponse, error) {
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: ctrlobs.SpanGatewayResolveServiceRoute,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrServiceID, req.GetServiceID()),
			attribute.String(sdkobs.AttrNamespace, req.GetNamespace()),
		},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "service_route")},
		Counter:     ctrlobs.MetricGatewayResolveTotal,
		Duration:    ctrlobs.MetricGatewayResolveDuration,
	})
	var err error
	defer func() { op.End(err) }()
	ttl := time.Duration(req.GetTtlSeconds()) * time.Second
	if ttl <= 0 {
		ttl = s.deps.DefaultTTL
	}
	resp, err := s.deps.Resolver.ResolveServiceRoute(ctx, req, ttl, s.now())
	if err != nil {
		op.SetErrorStatus("resolve service route")
		return nil, err
	}
	return resp, nil
}

func (s *Server) ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	// The interceptor admits this private RPC only from gatewayd. NodeSandbox
	// calls carry the external Principal fingerprint and require namespace
	// authorization. Gateway-owned HTTP and SSH terminals intentionally carry no
	// fingerprint because gatewayd has already authenticated their bearer token
	// or authorized key.
	if req.GetClientCertificateFingerprint() != "" {
		if s.deps.Access == nil {
			return nil, status.Error(codes.Unavailable, "authorization is not configured")
		}
		if err := s.deps.Access.AuthorizeFingerprintResource(ctx, req.GetClientCertificateFingerprint(), req.GetRolloutExecutionLease(), accesskernel.ActionSandboxExecute, "allocation", req.GetAllocationID()); err != nil {
			switch {
			case errors.Is(err, accesskernel.ErrUnauthenticated):
				return nil, status.Error(codes.Unauthenticated, "client credential is not active")
			case errors.Is(err, accesskernel.ErrPermissionDenied):
				return nil, status.Error(codes.PermissionDenied, "permission denied")
			case errors.Is(err, accesskernel.ErrNotFound):
				return nil, status.Error(codes.NotFound, "allocation not found")
			default:
				return nil, err
			}
		}
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        ctrlobs.SpanGatewayResolveAllocationTerminal,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrAllocationID, req.GetAllocationID())},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "allocation_terminal")},
		Counter:     ctrlobs.MetricGatewayTerminalResolveTotal,
		Duration:    ctrlobs.MetricGatewayTerminalResolveDuration,
	})
	var err error
	defer func() { op.End(err) }()
	ttl := time.Duration(req.GetTtlSeconds()) * time.Second
	if ttl <= 0 {
		ttl = s.deps.DefaultTTL
	}
	resp, err := s.deps.Resolver.ResolveAllocationTerminal(ctx, req, ttl, s.now())
	if err != nil {
		op.SetErrorStatus("resolve allocation terminal")
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrNodeID, resp.GetNodeID()))
	return resp, nil
}

func (s *Server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now().UTC()
}
