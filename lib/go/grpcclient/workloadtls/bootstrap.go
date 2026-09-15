package workloadtls

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Bootstrap owns deployment identity material, not Node admission. It never
// generates Node keys. SignerBundle must be mounted only by controld; workloads
// receive only their own bundle and the public CA. Existing authority is never
// replaced implicitly. RenewServices rotates service leaves under that same CA.
type Bootstrap struct {
	Directory, Cluster string
	DNSNames           []string
	IPAddresses        []net.IP
}

func (b Bootstrap) Ensure(renewServices bool) error {
	if _, err := (Identity{Cluster: b.Cluster, Role: "controld"}).URI(); err != nil {
		return err
	}
	if b.Directory == "" {
		return fmt.Errorf("PKI directory is required")
	}
	if err := os.MkdirAll(filepath.Join(b.Directory, "private"), 0700); err != nil {
		return err
	}
	// Provisioning may be invoked by the CLI and deployment tooling at once.
	// Kernel-owned locking releases on crash and never creates a stale lock owner.
	lock, err := os.OpenFile(filepath.Join(b.Directory, ".bootstrap.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("PKI provisioning is already active or cannot be locked: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	signerPath := filepath.Join(b.Directory, "private", "signer.pem")
	if _, err := os.Stat(signerPath); os.IsNotExist(err) {
		// Never regenerate a missing authority underneath existing certificates.
		for _, name := range []string{"ca.crt", "controld.pem", "gatewayd.pem", "tunneld.pem", "client.crt", "client.key"} {
			if _, err := os.Stat(filepath.Join(b.Directory, name)); !os.IsNotExist(err) {
				return fmt.Errorf("PKI authority missing with existing identity material; restore its backup")
			}
		}
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
		if err != nil {
			return err
		}
		serial.Add(serial, big.NewInt(1))
		now := time.Now().UTC()
		ca := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Axern " + b.Cluster}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
		if err != nil {
			return err
		}
		keyPEM, err := encodeSigner(key)
		if err != nil {
			return err
		}
		if err := PublishBundle(signerPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	issuer, err := (FileIssuer{BundlePath: signerPath, Cluster: b.Cluster}).Load()
	if err != nil {
		return err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuer.Certificate.Raw})
	if existing, err := os.ReadFile(filepath.Join(b.Directory, "ca.crt")); err == nil {
		block, _ := pem.Decode(existing)
		if block == nil || string(block.Bytes) != string(issuer.Certificate.Raw) {
			return fmt.Errorf("public CA does not match signing authority; explicit trust rotation is required")
		}
	} else if os.IsNotExist(err) {
		if err := publishPublic(filepath.Join(b.Directory, "ca.crt"), caPEM); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, role := range []string{"controld", "gatewayd", "tunneld"} {
		path := filepath.Join(b.Directory, role+".pem")
		identity := Identity{Cluster: b.Cluster, Role: role}
		if data, err := os.ReadFile(path); err == nil {
			pair, err := tls.X509KeyPair(data, data)
			if err != nil {
				return fmt.Errorf("invalid %s bundle: %w", role, err)
			}
			cert, err := x509.ParseCertificate(pair.Certificate[0])
			if err != nil {
				return err
			}
			id, err := FromCertificate(cert, b.Cluster)
			if err != nil || id != identity || cert.CheckSignatureFrom(issuer.Certificate) != nil {
				return fmt.Errorf("%s workload authority mismatch", role)
			}
			if !renewServices {
				for _, name := range b.DNSNames {
					if err := cert.VerifyHostname(name); err != nil {
						return fmt.Errorf("%s certificate requires explicit renewal for DNS name %s", role, name)
					}
				}
				for _, address := range b.IPAddresses {
					if err := cert.VerifyHostname(address.String()); err != nil {
						return fmt.Errorf("%s certificate requires explicit renewal for IP address %s", role, address)
					}
				}
				if time.Until(cert.NotAfter) < 24*time.Hour {
					return fmt.Errorf("%s certificate requires explicit service renewal", role)
				}
				continue
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		uri, err := identity.URI()
		if err != nil {
			return err
		}
		cert, key, err := b.issue(issuer, role, []*url.URL{uri}, true)
		if err != nil {
			return err
		}
		if err := PublishBundle(path, cert, key); err != nil {
			return err
		}
	}
	// The external administrator is a Principal credential, not a workload.
	// Never rotate its fingerprint as a side effect of service renewal.
	clientBundle := filepath.Join(b.Directory, "private", "client.pem")
	data, err := os.ReadFile(clientBundle)
	if os.IsNotExist(err) {
		for _, name := range []string{"client.crt", "client.key"} {
			if _, err := os.Stat(filepath.Join(b.Directory, name)); !os.IsNotExist(err) {
				return fmt.Errorf("administrator bundle missing with existing credential; restore its backup")
			}
		}
		cert, key, err := b.issue(issuer, "axern-bootstrap-admin", nil, false)
		if err != nil {
			return err
		}
		if err := PublishBundle(clientBundle, cert, key); err != nil {
			return err
		}
		data, err = os.ReadFile(clientBundle)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	if cert.CheckSignatureFrom(issuer.Certificate) != nil || len(cert.URIs) != 0 || !time.Now().Before(cert.NotAfter) {
		return fmt.Errorf("invalid bootstrap administrator authority")
	}
	keyPEM, err := encodeSigner(pair.PrivateKey.(crypto.Signer))
	if err != nil {
		return err
	}
	if err := publishPublic(filepath.Join(b.Directory, "client.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})); err != nil {
		return err
	}
	return publishPublic(filepath.Join(b.Directory, "client.key"), keyPEM)
}

func (b Bootstrap) issue(issuer Issuer, name string, uris []*url.URL, server bool) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
	if err != nil {
		return nil, nil, err
	}
	serial.Add(serial, big.NewInt(1))
	now := time.Now().UTC()
	until := now.AddDate(0, 0, 90)
	if until.After(issuer.Certificate.NotAfter) || now.Before(issuer.Certificate.NotBefore) {
		return nil, nil, fmt.Errorf("signing authority requires rotation")
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: until, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: uris}
	if server {
		leaf.ExtKeyUsage = append(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
		leaf.DNSNames = append(append([]string{}, b.DNSNames...), name)
		leaf.IPAddresses = b.IPAddresses
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, issuer.Certificate, &key.PublicKey, issuer.Key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeSigner(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM, err
}

func encodeSigner(key crypto.Signer) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func publishPublic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pki-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
