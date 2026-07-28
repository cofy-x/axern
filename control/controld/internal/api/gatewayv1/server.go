package gatewayv1

import (
	"context"
	"time"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"go.opentelemetry.io/otel/attribute"
)

type Resolver interface {
	ResolveServiceRoute(ctx context.Context, req *gatewayv1.ResolveServiceRouteRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveServiceRouteResponse, error)
	ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type Dependencies struct {
	Now        func() time.Time
	Resolver   Resolver
	DefaultTTL time.Duration
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
