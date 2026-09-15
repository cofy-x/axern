package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestNodeIngressAuthoritySeparatesControlAndGatewayServices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		identity string
		method   string
		wantCode codes.Code
	}{
		{name: "controld lifecycle", identity: "controld", method: "/axern.private.node.lifecycle.v1.NodeLifecycle/CreateAllocation", wantCode: codes.OK},
		{name: "gateway sandbox", identity: "gatewayd", method: "/axern.node.sandbox.v1.NodeSandbox/Process", wantCode: codes.OK},
		{name: "gateway cannot mutate lifecycle", identity: "gatewayd", method: "/axern.private.node.lifecycle.v1.NodeLifecycle/DeleteAllocation", wantCode: codes.PermissionDenied},
		{name: "controld cannot access sandbox", identity: "controld", method: "/axern.node.sandbox.v1.NodeSandbox/Exec", wantCode: codes.PermissionDenied},
		{name: "unverified caller rejected", method: "/axern.node.sandbox.v1.NodeSandbox/Exec", wantCode: codes.Unauthenticated},
		{name: "unknown service unavailable", identity: "controld", method: "/axern.private.node.operator.v1.NodeOperator/Exec", wantCode: codes.PermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.identity != "" {
				ctx = verifiedPeerContext(ctx, tt.identity)
			}
			if got := status.Code(authorizeNodeIngress(ctx, tt.method, "cluster.test")); got != tt.wantCode {
				t.Fatalf("authorizeNodeIngress() code = %v, want %v", got, tt.wantCode)
			}
		})
	}
}

func verifiedPeerContext(ctx context.Context, commonName string) context.Context {
	uri, _ := url.Parse("spiffe://cluster.test/service/" + commonName)
	certificate := &x509.Certificate{URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	return peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{certificate}},
	}}})
}
