package nodev1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type enrollmentControlStub struct{ calls int }

func (s *enrollmentControlStub) Enroll(context.Context, nodekernel.EnrollmentRequest) ([]byte, error) {
	s.calls++
	return []byte("certificate"), nil
}
func (s *enrollmentControlStub) Renew(context.Context, string, []byte, time.Time) ([]byte, error) {
	s.calls++
	return []byte("certificate"), nil
}

func TestEnrollmentRequiresTLSAndRenewalRequiresExactNode(t *testing.T) {
	for _, test := range []struct {
		name, uri, cn string
		verified      bool
		want          codes.Code
	}{
		{name: "exact Node", uri: "spiffe://cluster.test/node/node-one", verified: true, want: codes.OK},
		{name: "other Node", uri: "spiffe://cluster.test/node/node-two", verified: true, want: codes.PermissionDenied},
		{name: "other cluster", uri: "spiffe://other.test/node/node-one", verified: true, want: codes.Unauthenticated},
		{name: "service role", uri: "spiffe://cluster.test/service/controld", verified: true, want: codes.PermissionDenied},
		{name: "CN only", cn: "axern-node", verified: true, want: codes.Unauthenticated},
		{name: "unverified", uri: "spiffe://cluster.test/node/node-one", want: codes.Unauthenticated},
	} {
		t.Run(test.name, func(t *testing.T) {
			cert := &x509.Certificate{Subject: pkix.Name{CommonName: test.cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
			if test.uri != "" {
				u, err := url.Parse(test.uri)
				if err != nil {
					t.Fatal(err)
				}
				cert.URIs = []*url.URL{u}
			}
			state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
			if test.verified {
				state.VerifiedChains = [][]*x509.Certificate{{cert}}
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: state}})
			control := &enrollmentControlStub{}
			server := &EnrollmentServer{Control: control, Cluster: "cluster.test", Now: time.Now}
			_, err := server.RenewNodeCertificate(ctx, &nodev1.RenewNodeCertificateRequest{NodeID: "node-one", CsrDer: []byte("csr")})
			if status.Code(err) != test.want {
				t.Fatalf("code=%v err=%v", status.Code(err), err)
			}
			if test.want != codes.OK && control.calls != 0 {
				t.Fatal("unauthorized request reached signer")
			}
		})
	}
	control := &enrollmentControlStub{}
	server := &EnrollmentServer{Control: control, Cluster: "cluster.test", Now: time.Now}
	if _, err := server.EnrollNode(context.Background(), &nodev1.EnrollNodeRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("plaintext enrollment: %v", err)
	}
	if control.calls != 0 {
		t.Fatal("plaintext request reached enrollment store")
	}
}

func TestRenewalRejectsExpiredAuthorityOnExistingConnection(t *testing.T) {
	now := time.Now()
	uri, _ := url.Parse("spiffe://cluster.test/node/node-one")
	for _, expiredLeaf := range []bool{true, false} {
		leaf := &x509.Certificate{URIs: []*url.URL{uri}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
		ca := &x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
		if expiredLeaf {
			leaf.NotAfter = now
		} else {
			ca.NotAfter = now
		}
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf, ca}}}}})
		control := &enrollmentControlStub{}
		server := &EnrollmentServer{Control: control, Cluster: "cluster.test", Now: func() time.Time { return now }}
		_, err := server.RenewNodeCertificate(ctx, &nodev1.RenewNodeCertificateRequest{NodeID: "node-one"})
		if status.Code(err) != codes.Unauthenticated || control.calls != 0 {
			t.Fatalf("expired authority reached signer: %v", err)
		}
	}
}
