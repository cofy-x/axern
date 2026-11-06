package controlv1

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMissingClientMaterialIsConfigurationError(t *testing.T) {
	directory := t.TempDir()
	_, err := dialControlGRPC(context.Background(), Config{TLSCert: filepath.Join(directory, "missing.crt"), TLSKey: filepath.Join(directory, "missing.key")})
	var configuration ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("missing credentials error = %v, want ConfigurationError", err)
	}
}

func TestClientCertificateTimeRejectsExpiredOrFutureChain(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := func(start, end time.Time) []byte {
		t.Helper()
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: start, NotAfter: end}
		raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	valid := certificate(now.Add(-time.Hour), now.Add(time.Hour))
	expired := certificate(now.Add(-2*time.Hour), now.Add(-time.Hour))
	for _, tc := range []struct {
		name  string
		chain [][]byte
		valid bool
	}{
		{"valid", [][]byte{valid}, true},
		{"expired leaf", [][]byte{expired}, false},
		{"expired intermediate", [][]byte{valid, expired}, false},
		{"future leaf", [][]byte{certificate(now.Add(time.Hour), now.Add(2*time.Hour))}, false},
		{"expiry boundary", [][]byte{certificate(now.Add(-time.Hour), now)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateClientCertificateTime(tls.Certificate{Certificate: tc.chain}, now)
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %v; error = %v", tc.valid, err)
			}
		})
	}
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key")
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: expired}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = dialControlGRPC(context.Background(), Config{TLSCert: certPath, TLSKey: keyPath})
	var configuration ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("expired certificate error = %v, want ConfigurationError before dialing", err)
	}
}
