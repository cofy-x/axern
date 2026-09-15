package nodev1

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	pgnodes "github.com/cofy-x/axern/control/controld/internal/postgres/nodes"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// This is a real TLS/gRPC/Postgres path, not an injected peer or store. Keeping
// it in the Postgres gate makes receipt recovery and revocation part of the
// same local verification entrypoint as all other control-plane transactions.
func TestNodeEnrollmentTLSPostgresE2E(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	issuer := workloadtls.Issuer{Certificate: ca, Key: caKey, Cluster: "cluster.test"}
	dir := t.TempDir()
	trustPath, signerPath, serverPath := filepath.Join(dir, "trust.pem"), filepath.Join(dir, "signer.pem"), filepath.Join(dir, "controld.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	if err := os.WriteFile(trustPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	keyPEM := func(key *ecdsa.PrivateKey) []byte {
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	}
	if err := workloadtls.PublishBundle(signerPath, caPEM, keyPEM(caKey)); err != nil {
		t.Fatal(err)
	}
	newCSR := func() (*ecdsa.PrivateKey, []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
		if err != nil {
			t.Fatal(err)
		}
		return key, csr
	}
	serverID := workloadtls.Identity{Cluster: issuer.Cluster, Role: "controld"}
	serverKey, serverCSR := newCSR()
	serverCert, err := issuer.Sign(serverCSR, serverID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := workloadtls.PublishBundle(serverPath, serverCert, keyPEM(serverKey)); err != nil {
		t.Fatal(err)
	}
	token := "test-only-one-time-enrollment-token-000001"
	admin := pgadmin.NewStore(db)
	if _, err := admin.AdmitNode(ctx, adminkernel.AdmitNodeRequest{NodeID: "node-one", EnrollmentToken: token, OperatorReason: "e2e initial admission", Now: now}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(&workloadtls.EnrollmentCredentials{Workload: &workloadtls.Credentials{BundlePath: serverPath, TrustPath: trustPath, Local: serverID}}), grpc.MaxRecvMsgSize(32<<10))
	nodev1.RegisterNodeEnrollmentServer(server, &EnrollmentServer{Control: &appnode.Enrollment{Store: pgnodes.NewPGStore(db), Issuer: workloadtls.FileIssuer{BundlePath: signerPath, Cluster: issuer.Cluster}}, Cluster: issuer.Cluster, Now: time.Now})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connect := func(transport credentials.TransportCredentials) nodev1.NodeEnrollmentClient {
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(transport), grpc.WithNoProxy())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return nodev1.NewNodeEnrollmentClient(conn)
	}
	bootstrap := connect(&workloadtls.BootstrapCredentials{TrustPath: trustPath, Peer: serverID})
	nodeKey, csr := newCSR()
	req := &nodev1.EnrollNodeRequest{NodeID: "node-one", EnrollmentToken: token, CsrDer: csr}
	first, err := bootstrap.EnrollNode(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// A process/transport restart with the persisted CSR recovers the committed
	// response without signing a second certificate.
	retryClient := connect(&workloadtls.BootstrapCredentials{TrustPath: trustPath, Peer: serverID})
	retry, err := retryClient.EnrollNode(ctx, req)
	if err != nil || !bytes.Equal(first.GetCertificatePem(), retry.GetCertificatePem()) {
		t.Fatalf("retry changed certificate: %v", err)
	}
	_, otherCSR := newCSR()
	if _, err := bootstrap.EnrollNode(ctx, &nodev1.EnrollNodeRequest{NodeID: "node-one", EnrollmentToken: token, CsrDer: otherCSR}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("replay with new key: %v", err)
	}
	renewReq := &nodev1.RenewNodeCertificateRequest{NodeID: "node-one", CsrDer: csr}
	if _, err := bootstrap.RenewNodeCertificate(ctx, renewReq); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("token-only renewal: %v", err)
	}
	nodePath := filepath.Join(dir, "node.pem")
	if err := workloadtls.PublishBundle(nodePath, first.GetCertificatePem(), keyPEM(nodeKey)); err != nil {
		t.Fatal(err)
	}
	nodeID := workloadtls.Identity{Cluster: issuer.Cluster, Role: "axnoded", NodeID: "node-one"}
	transport := &workloadtls.Credentials{BundlePath: nodePath, TrustPath: trustPath, Local: nodeID, Peer: serverID}
	node := connect(transport)
	renewed, err := node.RenewNodeCertificate(ctx, renewReq)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(renewed.GetCertificatePem(), first.GetCertificatePem()) {
		t.Fatal("renewal reused certificate")
	}
	if err := workloadtls.PublishBundle(nodePath, renewed.GetCertificatePem(), keyPEM(nodeKey)); err != nil {
		t.Fatal(err)
	}
	rotated := connect(transport.Clone())
	if _, err := rotated.RenewNodeCertificate(ctx, renewReq); err != nil {
		t.Fatalf("rotated identity: %v", err)
	}
	if _, err := node.RenewNodeCertificate(ctx, &nodev1.RenewNodeCertificateRequest{NodeID: "node-two", CsrDer: csr}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-node renewal: %v", err)
	}
	if _, err := admin.RevokeNode(ctx, adminkernel.RevokeNodeRequest{NodeID: "node-one", OperatorReason: "e2e revocation", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Revocation must also reject an already established TLS connection.
	if _, err := node.RenewNodeCertificate(ctx, renewReq); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked renewal on old connection: %v", err)
	}
	if _, err := retryClient.EnrollNode(ctx, req); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked replay: %v", err)
	}
}
