package gatewayv1

import (
	"context"
	"errors"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Resolver interface {
	ValidateAllocationAccess(context.Context, *gatewayv1.ResolveAllocationTerminalRequest, time.Time) error
	ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type TunnelResolver interface {
	ResolveRelayTarget(context.Context, string, time.Time) (string, error)
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
	target, err := s.deps.Tunnels.ResolveRelayTarget(ctx, req.GetSessionID(), s.now())
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ResolveTunnelRelayTargetResponse{NodeEdgeTarget: target}, nil
}

type AccessAuthorizer interface {
	AuthorizeCredentialResource(context.Context, string, accesskernel.CredentialKind, accesskernel.Action, string, string) error
}

type Server struct {
	gatewayv1.UnimplementedGatewayControlServer
	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}

func (s *Server) ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	if err := s.authorizeCredential(ctx, req); err != nil {
		return nil, err
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
	if req.GetTtlSeconds() > int64((2*time.Hour)/time.Second) || ttl > 2*time.Hour {
		return nil, status.Error(codes.InvalidArgument, "allocation access grant cannot exceed two hours")
	}
	resp, err := s.deps.Resolver.ResolveAllocationTerminal(ctx, req, ttl, s.now())
	if err != nil {
		op.SetErrorStatus("resolve allocation terminal")
		return nil, err
	}
	op.SetAttributes(attribute.String(sdkobs.AttrNodeID, resp.GetNodeID()))
	return resp, nil
}

func (s *Server) AuthorizeAllocationAccess(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) (*emptypb.Empty, error) {
	if err := s.authorizeCredential(ctx, req); err != nil {
		return nil, err
	}
	if err := s.deps.Resolver.ValidateAllocationAccess(ctx, req, s.now()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) authorizeCredential(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest) error {
	if s.deps.Access == nil {
		return status.Error(codes.Unavailable, "authorization is not configured")
	}
	action := accesskernel.ActionSandboxExecute
	if req.GetPurpose() == gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT {
		action = accesskernel.ActionResourceRead
	}
	if req.GetCredentialFingerprint() == "" {
		return status.Error(codes.Unauthenticated, "client credential is required")
	}
	err := s.deps.Access.AuthorizeCredentialResource(ctx, req.GetCredentialFingerprint(), accesskernel.CredentialKind(req.GetCredentialKind()), action, "allocation", req.GetAllocationID())
	switch {
	case errors.Is(err, accesskernel.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "client credential is not active")
	case errors.Is(err, accesskernel.ErrPermissionDenied), errors.Is(err, accesskernel.ErrNotFound):
		return status.Error(codes.NotFound, "allocation not found")
	case err != nil:
		return err
	}
	return nil
}

func (s *Server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now().UTC()
}
