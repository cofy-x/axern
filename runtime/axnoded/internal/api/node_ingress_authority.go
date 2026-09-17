package api

import (
	"context"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"strings"
	"time"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	controldPeerIdentity = "controld"
	gatewaydPeerIdentity = "gatewayd"
)

type NodeIngressAuthority struct{ cluster string }

func NewNodeIngressAuthority(cluster string) NodeIngressAuthority {
	return NodeIngressAuthority{cluster: cluster}
}

func (a NodeIngressAuthority) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := authorizeNodeIngress(ctx, info.FullMethod, a.cluster); err != nil {
		return nil, err
	}
	_, deadline, err := workloadtls.PeerIdentity(ctx, a.cluster, time.Now())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "current workload identity is required")
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return handler(ctx, req)
}

func (a NodeIngressAuthority) Stream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := authorizeNodeIngress(stream.Context(), info.FullMethod, a.cluster); err != nil {
		return err
	}
	_, deadline, err := workloadtls.PeerIdentity(stream.Context(), a.cluster, time.Now())
	if err != nil {
		return status.Error(codes.Unauthenticated, "current workload identity is required")
	}
	ctx, cancel := context.WithDeadline(stream.Context(), deadline)
	defer cancel()
	return handler(srv, &identityDeadlineStream{ServerStream: stream, ctx: ctx})
}

func authorizeNodeIngress(ctx context.Context, fullMethod, cluster string) error {
	want := ""
	switch {
	case strings.HasPrefix(fullMethod, "/"+nodelifecyclev1.NodeLifecycle_ServiceDesc.ServiceName+"/"):
		want = controldPeerIdentity
	case strings.HasPrefix(fullMethod, "/"+nodesandboxv1.NodeSandbox_ServiceDesc.ServiceName+"/"):
		want = gatewaydPeerIdentity
	case strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/"):
		identity := nodeIngressPeerIdentity(ctx, cluster)
		if identity == controldPeerIdentity || identity == gatewaydPeerIdentity {
			return nil
		}
		if identity == "" {
			return status.Error(codes.Unauthenticated, "verified mTLS identity is required")
		}
		return status.Error(codes.PermissionDenied, "mTLS identity is not authorized for node health")
	default:
		return status.Error(codes.PermissionDenied, "service is not available on the node ingress listener")
	}
	identity := nodeIngressPeerIdentity(ctx, cluster)
	if identity == "" {
		return status.Error(codes.Unauthenticated, "verified mTLS identity is required")
	}
	if identity != want {
		return status.Errorf(codes.PermissionDenied, "%s mTLS identity is required", want)
	}
	return nil
}

func nodeIngressPeerIdentity(ctx context.Context, cluster string) string {
	identity, _, err := workloadtls.PeerIdentity(ctx, cluster, time.Now())
	if err != nil {
		return ""
	}
	return identity.Role
}

type identityDeadlineStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *identityDeadlineStream) Context() context.Context { return s.ctx }
