package controlplane

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
)

// NodeEnrollment owns only local TLS credentials and the exact pending CSR
// needed to recover a lost first-registration reply. It owns no runtime state.
// One node process must hold the state directory before invoking its methods.
type NodeEnrollment struct {
	Client     nodev1.NodeEnrollmentClient
	Identity   workloadtls.Identity
	BundlePath string
	TrustPath  string
}

type pendingNodeEnrollment struct {
	KeyPEM []byte `json:"key_pem"`
	CSRDER []byte `json:"csr_der"`
}

func (n *NodeEnrollment) Ensure(ctx context.Context, token string) error {
	if _, err := os.Stat(n.BundlePath); err == nil {
		_, err = n.current()
		if err != nil {
			return err
		}
		return n.removePending()
	} else if !os.IsNotExist(err) {
		return err
	}
	pendingPath := n.BundlePath + ".pending"
	data, err := os.ReadFile(pendingPath)
	var pending pendingNodeEnrollment
	if err == nil {
		if err := json.Unmarshal(data, &pending); err != nil {
			return fmt.Errorf("invalid pending node enrollment: %w", err)
		}
	} else if os.IsNotExist(err) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		pending.CSRDER, err = x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
		if err != nil {
			return err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return err
		}
		pending.KeyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		data, err = json.Marshal(pending)
		if err != nil {
			return err
		}
		if err := persistPendingEnrollment(pendingPath, data); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := validatePendingEnrollment(pending); err != nil {
		return err
	}
	response, err := n.Client.EnrollNode(ctx, &nodev1.EnrollNodeRequest{NodeID: n.Identity.NodeID, EnrollmentToken: token, CsrDer: pending.CSRDER})
	if err != nil {
		return err
	}
	if err := n.publish(response.GetCertificatePem(), pending.KeyPEM); err != nil {
		return err
	}
	// A leftover pending CSR cannot trigger enrollment while a valid bundle
	// exists. Keep errors visible; retries load the published bundle first.
	return n.removePending()
}

func (n *NodeEnrollment) removePending() error {
	err := os.Remove(n.BundlePath + ".pending")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(n.BundlePath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func validatePendingEnrollment(p pendingNodeEnrollment) error {
	block, rest := pem.Decode(p.KeyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return fmt.Errorf("invalid pending Node private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("invalid pending Node private key")
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return fmt.Errorf("invalid pending Node signer")
	}
	csr, err := x509.ParseCertificateRequest(p.CSRDER)
	if err != nil || csr.CheckSignature() != nil {
		return fmt.Errorf("invalid pending Node CSR")
	}
	public, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return err
	}
	if !bytes.Equal(public, csr.RawSubjectPublicKeyInfo) {
		return fmt.Errorf("pending Node key and CSR do not match")
	}
	return nil
}

func (n *NodeEnrollment) current() (*tls.Certificate, error) {
	data, err := os.ReadFile(n.BundlePath)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, err
	}
	if err := n.validate(&pair); err != nil {
		return nil, err
	}
	return &pair, nil
}

func (n *NodeEnrollment) validate(pair *tls.Certificate) error {
	if n.Identity.Role != "axnoded" {
		return fmt.Errorf("node enrollment requires a Node identity")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	identity, err := workloadtls.FromCertificate(leaf, n.Identity.Cluster)
	if err != nil || identity != n.Identity {
		return fmt.Errorf("issued Node identity mismatch")
	}
	trust, err := os.ReadFile(n.TrustPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return fmt.Errorf("invalid node trust bundle")
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return err
		}
		intermediates.AddCert(cert)
	}
	for _, usage := range []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
			return err
		}
	}
	pair.Leaf = leaf
	return nil
}

func (n *NodeEnrollment) publish(cert, key []byte) error {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return err
	}
	if err := n.validate(&pair); err != nil {
		return err
	}
	return workloadtls.PublishBundle(n.BundlePath, cert, key)
}

// RenewIfDue is driven by the node's single credential-maintenance loop. The
// current mTLS certificate authenticates renewal; the enrollment token is absent.
func (n *NodeEnrollment) RenewIfDue(ctx context.Context) error {
	pair, err := n.current()
	if err != nil {
		return err
	}
	if time.Until(pair.Leaf.NotAfter) > 8*time.Hour {
		return nil
	}
	// Renewal is authenticated by the current bundle, but rotates the key as
	// well as the leaf. A lost reply leaves the old valid bundle intact; another
	// authorized renewal can safely retry without a second durable intent.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		return err
	}
	response, err := n.Client.RenewNodeCertificate(ctx, &nodev1.RenewNodeCertificateRequest{NodeID: n.Identity.NodeID, CsrDer: csr})
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return n.publish(response.GetCertificatePem(), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func persistPendingEnrollment(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".node-enrollment-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
