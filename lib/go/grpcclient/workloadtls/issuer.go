package workloadtls

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"time"
)

const LeafLifetime = 24 * time.Hour

type Issuer struct {
	Certificate *x509.Certificate
	Key         crypto.Signer
	Cluster     string
}

// Sign accepts only the CSR public key. Identity, SANs, usages and lifetime are
// chosen by the admitted owner, never copied from caller-controlled extensions.
func (i Issuer) Sign(csrDER []byte, identity Identity, now time.Time) ([]byte, error) {
	if len(csrDER) == 0 || len(csrDER) > 16<<10 {
		return nil, fmt.Errorf("invalid CSR size")
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}
	switch key := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if key.Curve.Params().BitSize < 256 {
			return nil, fmt.Errorf("weak CSR key")
		}
	case *rsa.PublicKey:
		if key.N.BitLen() < 2048 {
			return nil, fmt.Errorf("weak CSR key")
		}
	case ed25519.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported CSR key")
	}
	if i.Certificate == nil || i.Key == nil || !i.Certificate.IsCA || !i.Certificate.BasicConstraintsValid || i.Certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, fmt.Errorf("invalid workload signing authority")
	}
	if identity.Cluster != i.Cluster {
		return nil, fmt.Errorf("workload trust domain mismatch")
	}
	u, err := identity.URI()
	if err != nil {
		return nil, err
	}
	until := now.Add(LeafLifetime)
	if now.Before(i.Certificate.NotBefore) || until.After(i.Certificate.NotAfter) {
		return nil, fmt.Errorf("workload signing authority requires rotation")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
	if err != nil {
		return nil, err
	}
	serial.Add(serial, big.NewInt(1))
	template := &x509.Certificate{SerialNumber: serial, NotBefore: now.Add(-time.Minute), NotAfter: until, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, URIs: []*url.URL{u}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, i.Certificate, csr.PublicKey, i.Key)
	if err != nil {
		return nil, fmt.Errorf("sign workload certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
