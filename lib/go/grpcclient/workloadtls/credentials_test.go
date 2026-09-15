package workloadtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
)

func testIssuer(t *testing.T) Issuer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return Issuer{Certificate: cert, Key: key, Cluster: "cluster.test"}
}

func testBundle(t *testing.T, i Issuer, id Identity) ([]byte, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := i.Sign(csr, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), cert
}

func TestCredentialsVerifyExactPeerAndReloadAtomically(t *testing.T) {
	issuer := testIssuer(t)
	dir := t.TempDir()
	local := Identity{Cluster: issuer.Cluster, Role: "gatewayd"}
	remote := Identity{Cluster: issuer.Cluster, Role: "axnoded", NodeID: "node-one"}
	cert, key, _ := testBundle(t, issuer, local)
	bundle := filepath.Join(dir, "identity.pem")
	if err := PublishBundle(bundle, cert, key); err != nil {
		t.Fatal(err)
	}
	trust := filepath.Join(dir, "trust.pem")
	if err := os.WriteFile(trust, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuer.Certificate.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	creds := &Credentials{BundlePath: bundle, TrustPath: trust, Local: local, Peer: remote}
	config, err := creds.config(false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, peer := testBundle(t, issuer, remote)
	if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}); err != nil {
		t.Fatal(err)
	}
	_, _, wrongNode := testBundle(t, issuer, Identity{Cluster: issuer.Cluster, Role: "axnoded", NodeID: "node-two"})
	if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{wrongNode}}); err == nil {
		t.Fatal("different Node accepted")
	}
	rogue := testIssuer(t)
	_, _, untrusted := testBundle(t, rogue, remote)
	if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{untrusted}}); err == nil {
		t.Fatal("untrusted signer accepted")
	}
	cert2, key2, _ := testBundle(t, issuer, local)
	if err := PublishBundle(bundle, cert2, key); err == nil {
		t.Fatal("mismatched key published")
	}
	if err := PublishBundle(bundle, cert2, key2); err != nil {
		t.Fatal(err)
	}
	next, err := creds.config(false)
	if err != nil {
		t.Fatal(err)
	}
	oldLeaf, _ := x509.ParseCertificate(config.Certificates[0].Certificate[0])
	newLeaf, _ := x509.ParseCertificate(next.Certificates[0].Certificate[0])
	if oldLeaf.SerialNumber.Cmp(newLeaf.SerialNumber) == 0 {
		t.Fatal("handshake reused old credential")
	}
	info, err := os.Stat(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("bundle mode=%v", info.Mode())
	}
	if err := os.WriteFile(bundle, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := creds.config(false); err == nil {
		t.Fatal("invalid reload used old authority")
	}
}

func TestTrustRotationAcceptsOverlapThenRejectsOldSigner(t *testing.T) {
	oldCA, newCA := testIssuer(t), testIssuer(t)
	local := Identity{Cluster: oldCA.Cluster, Role: "controld"}
	remote := Identity{Cluster: oldCA.Cluster, Role: "axnoded", NodeID: "node-one"}
	dir := t.TempDir()
	bundle, trust := filepath.Join(dir, "identity.pem"), filepath.Join(dir, "trust.pem")
	cert, key, _ := testBundle(t, oldCA, local)
	if err := PublishBundle(bundle, cert, key); err != nil {
		t.Fatal(err)
	}
	_, _, oldPeer := testBundle(t, oldCA, remote)
	_, _, newPeer := testBundle(t, newCA, remote)
	encode := func(i Issuer) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: i.Certificate.Raw})
	}
	creds := &Credentials{BundlePath: bundle, TrustPath: trust, Local: local, Peer: remote}
	if err := publishPublic(trust, append(encode(oldCA), encode(newCA)...)); err != nil {
		t.Fatal(err)
	}
	config, err := creds.config(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []*x509.Certificate{oldPeer, newPeer} {
		if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}); err != nil {
			t.Fatal(err)
		}
	}
	cert, key, _ = testBundle(t, newCA, local)
	if err := PublishBundle(bundle, cert, key); err != nil {
		t.Fatal(err)
	}
	if err := publishPublic(trust, encode(newCA)); err != nil {
		t.Fatal(err)
	}
	config, err = creds.config(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{oldPeer}}); err == nil {
		t.Fatal("removed trust still accepted old signer")
	}
	if err := config.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{newPeer}}); err != nil {
		t.Fatal(err)
	}
}

func TestTransportLifetimeCannotOutliveCertificate(t *testing.T) {
	issuer := testIssuer(t)
	cert, key, _ := testBundle(t, issuer, Identity{Cluster: issuer.Cluster, Role: "controld"})
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	expires := time.Now().Add(100 * time.Millisecond)
	peer := &x509.Certificate{NotAfter: expires}
	auth := credentials.TLSInfo{State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{peer}}}}
	bounded, _, err := boundCertificateLifetime(server, auth, nil, pair)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	// gRPC clears the raw connection deadline after HTTP/2 setup. Certificate
	// expiry must still close it, even when no further RPC is received.
	if err := server.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := bounded.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if err == nil || time.Now().Before(expires) {
			t.Fatalf("unexpected early transport closure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clearing protocol deadline disabled certificate expiry")
	}
	peer.NotAfter = time.Now().Add(-time.Second)
	if _, _, err := boundCertificateLifetime(server, auth, nil, pair); err == nil {
		t.Fatal("expired transport accepted")
	}
}
