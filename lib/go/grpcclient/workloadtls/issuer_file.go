package workloadtls

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"
)

// FileIssuer reloads one atomically published CA certificate/key bundle for each
// issuance. Only the control plane may mount this bundle. A bad replacement
// fails closed; it never falls back to an old in-memory signing authority.
type FileIssuer struct{ BundlePath, Cluster string }

func (f FileIssuer) Sign(csr []byte, identity Identity, now time.Time) ([]byte, error) {
	i, err := f.Load()
	if err != nil {
		return nil, err
	}
	return i.Sign(csr, identity, now)
}

func (f FileIssuer) Load() (Issuer, error) {
	data, err := os.ReadFile(f.BundlePath)
	if err != nil {
		return Issuer{}, fmt.Errorf("read workload signing authority: %w", err)
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return Issuer{}, fmt.Errorf("parse workload signing authority: %w", err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return Issuer{}, err
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return Issuer{}, fmt.Errorf("invalid workload signing authority")
	}
	if _, err := (Identity{Cluster: f.Cluster, Role: "controld"}).URI(); err != nil {
		return Issuer{}, err
	}
	return Issuer{Certificate: cert, Key: key, Cluster: f.Cluster}, nil
}
