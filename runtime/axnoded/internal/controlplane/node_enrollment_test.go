package controlplane

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc"
)

type enrollmentClientFixture struct {
	issuer                  workloadtls.Issuer
	identity                workloadtls.Identity
	csr                     []byte
	response                []byte
	enrollCalls, renewCalls int
}

func (c *enrollmentClientFixture) EnrollNode(_ context.Context, req *nodev1.EnrollNodeRequest, _ ...grpc.CallOption) (*nodev1.EnrollNodeResponse, error) {
	c.enrollCalls++
	if c.csr == nil {
		c.csr = append([]byte{}, req.GetCsrDer()...)
		var err error
		c.response, err = c.issuer.Sign(c.csr, c.identity, time.Now().Add(-20*time.Hour))
		if err != nil {
			return nil, err
		}
		return nil, errors.New("response lost after commit")
	}
	if !bytes.Equal(c.csr, req.GetCsrDer()) {
		return nil, errors.New("CSR changed after restart")
	}
	return &nodev1.EnrollNodeResponse{CertificatePem: c.response}, nil
}
func (c *enrollmentClientFixture) RenewNodeCertificate(_ context.Context, req *nodev1.RenewNodeCertificateRequest, _ ...grpc.CallOption) (*nodev1.RenewNodeCertificateResponse, error) {
	c.renewCalls++
	cert, err := c.issuer.Sign(req.GetCsrDer(), c.identity, time.Now())
	if err != nil {
		return nil, err
	}
	return &nodev1.RenewNodeCertificateResponse{CertificatePem: cert}, nil
}

func TestNodeEnrollmentRecoversReplyAndRenewsWithoutToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.pem")
	bundle := filepath.Join(dir, "node.pem")
	if err := os.WriteFile(trust, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	identity := workloadtls.Identity{Cluster: "cluster.test", Role: "axnoded", NodeID: "node-one"}
	client := &enrollmentClientFixture{issuer: workloadtls.Issuer{Certificate: ca, Key: key, Cluster: identity.Cluster}, identity: identity}
	tokenFile := filepath.Join(dir, "bootstrap-token")
	if err := os.WriteFile(tokenFile, []byte("enrollment-token"), 0400); err != nil {
		t.Fatal(err)
	}
	first := &NodeEnrollment{Client: client, Identity: identity, BundlePath: bundle, TrustPath: trust}
	if err := first.Bootstrap(context.Background(), tokenFile); err == nil {
		t.Fatal("lost response not reported")
	}
	recovered := &NodeEnrollment{Client: client, Identity: identity, BundlePath: bundle, TrustPath: trust}
	if err := recovered.Bootstrap(context.Background(), tokenFile); err != nil {
		t.Fatal(err)
	}
	if client.enrollCalls != 2 {
		t.Fatalf("enrollment calls=%d", client.enrollCalls)
	}
	if err := os.Remove(tokenFile); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Bootstrap(context.Background(), tokenFile); err != nil {
		t.Fatalf("restart needs removed bootstrap token: %v", err)
	}
	before, err := recovered.current()
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.RenewIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := recovered.current()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(after.Leaf.RawSubjectPublicKeyInfo, before.Leaf.RawSubjectPublicKeyInfo) {
		t.Fatal("renewal did not rotate the Node private key")
	}
	if !after.Leaf.NotAfter.After(before.Leaf.NotAfter) || client.renewCalls != 1 {
		t.Fatal("certificate was not renewed")
	}
	if err := recovered.Ensure(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if client.enrollCalls != 2 {
		t.Fatal("restart replayed enrollment token")
	}
	if err := recovered.RenewIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.renewCalls != 1 {
		t.Fatal("fresh certificate renewed again")
	}
	if _, err := os.Stat(bundle + ".pending"); !os.IsNotExist(err) {
		t.Fatalf("pending private key remains: %v", err)
	}
	conflicting := *recovered
	conflicting.Identity.NodeID = "node-two"
	if err := conflicting.Bootstrap(context.Background(), tokenFile); err == nil {
		t.Fatal("existing identity rebound to another Node")
	}
	if client.enrollCalls != 2 {
		t.Fatal("identity conflict attempted re-enrollment")
	}
	if err := os.WriteFile(bundle, []byte("invalid certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Bootstrap(context.Background(), tokenFile); err == nil {
		t.Fatal("invalid published identity accepted")
	}
}
