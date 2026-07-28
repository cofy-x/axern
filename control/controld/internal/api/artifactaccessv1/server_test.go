package artifactaccessv1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestAuthorizeGatewayRequiresDedicatedVerifiedIdentity(t *testing.T) {
	verified := func(commonName string) context.Context {
		certificate := &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
		tlsInfo := credentials.TLSInfo{State: tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{certificate},
			VerifiedChains:   [][]*x509.Certificate{{certificate}},
		}}
		return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})
	}

	if err := authorizeGateway(verified("gatewayd")); err != nil {
		t.Fatalf("gatewayd identity rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "no peer", ctx: context.Background()},
		{name: "generic client", ctx: verified("axern-client")},
		{name: "worker", ctx: verified("axrun-worker")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := authorizeGateway(test.ctx); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("authorizeGateway() error = %v, want PermissionDenied", err)
			}
		})
	}
}
