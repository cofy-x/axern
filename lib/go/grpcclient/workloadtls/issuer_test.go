package workloadtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"
)

func TestIssuerUsesAdmittedIdentityNotCSRClaims(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := url.Parse("spiffe://cluster.test/service/controld")
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "controld"}, DNSNames: []string{"admin.internal"}, URIs: []*url.URL{forged}}, key)
	if err != nil {
		t.Fatal(err)
	}
	issuer := Issuer{Certificate: ca, Key: caKey, Cluster: "cluster.test"}
	identity := Identity{Cluster: "cluster.test", Role: "axnoded", NodeID: "node-one"}
	leafPEM, err := issuer.Sign(csr, identity, now)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(leafPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromCertificate(leaf, "cluster.test")
	if err != nil || got != identity {
		t.Fatalf("identity=%+v error=%v", got, err)
	}
	if leaf.Subject.CommonName != "" || len(leaf.DNSNames) != 0 || leaf.IsCA {
		t.Fatal("CSR escalated privileges")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
			t.Fatal(err)
		}
	}
	if !leaf.NotAfter.Equal(now.Add(LeafLifetime)) {
		t.Fatal("incorrect leaf lifetime")
	}
	if _, err := issuer.Sign(csr, Identity{Cluster: "other.test", Role: "axnoded", NodeID: "node-one"}, now); err == nil {
		t.Fatal("cross-cluster signing allowed")
	}
	if _, err := issuer.Sign(csr, identity, ca.NotAfter.Add(-time.Hour)); err == nil {
		t.Fatal("leaf outlives signer")
	}
	csr[len(csr)-1] ^= 1
	if _, err := issuer.Sign(csr, identity, now); err == nil {
		t.Fatal("invalid CSR signature accepted")
	}
}

func TestIdentityRejectsAmbiguityAndCNFallback(t *testing.T) {
	for _, raw := range []string{"spiffe://other.test/node/one", "spiffe://cluster.test/node/../one", "spiffe://cluster.test/node/one?role=controld", "spiffe://cluster.test/service/unknown", "https://cluster.test/node/one", "spiffe://cluster.test/node/%6fne"} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := FromCertificate(&x509.Certificate{URIs: []*url.URL{u}}, "cluster.test"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	u, _ := url.Parse("spiffe://cluster.test/service/controld")
	for _, cert := range []*x509.Certificate{nil, {Subject: pkix.Name{CommonName: "controld"}}, {URIs: []*url.URL{u, u}}} {
		if _, err := FromCertificate(cert, "cluster.test"); err == nil {
			t.Fatal("ambiguous/missing URI accepted")
		}
	}
}
