package api

import (
	"context"
	"crypto/x509"
	"strings"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	controldPeerIdentity = "controld"
	gatewaydPeerIdentity = "gatewayd"
)

type NodeIngressAuthority struct{}

func NewNodeIngressAuthority() NodeIngressAuthority { return NodeIngressAuthority{} }

func (NodeIngressAuthority) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := authorizeNodeIngress(ctx, info.FullMethod); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

func (NodeIngressAuthority) Stream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := authorizeNodeIngress(stream.Context(), info.FullMethod); err != nil {
		return err
	}
	return handler(srv, stream)
}

func authorizeNodeIngress(ctx context.Context, fullMethod string) error {
	want := ""
	switch {
	case strings.HasPrefix(fullMethod, "/"+nodelifecyclev1.NodeLifecycle_ServiceDesc.ServiceName+"/"):
		want = controldPeerIdentity
	case strings.HasPrefix(fullMethod, "/"+nodesandboxv1.NodeSandbox_ServiceDesc.ServiceName+"/"):
		want = gatewaydPeerIdentity
	case strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/"):
		identity := nodeIngressPeerIdentity(ctx)
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
	identity := nodeIngressPeerIdentity(ctx)
	if identity == "" {
		return status.Error(codes.Unauthenticated, "verified mTLS identity is required")
	}
	if identity != want {
		return status.Errorf(codes.PermissionDenied, "%s mTLS identity is required", want)
	}
	return nil
}

func nodeIngressPeerIdentity(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return ""
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return ""
	}
	return certificateCommonName(info.State.VerifiedChains[0][0])
}

func certificateCommonName(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return strings.TrimSpace(cert.Subject.CommonName)
}
