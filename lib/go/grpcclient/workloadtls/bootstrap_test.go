package workloadtls

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBootstrapRenewalPreservesAuthorityAndAdministrator(t *testing.T) {
	b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern", DNSNames: []string{"localhost"}}
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(b.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	ca, admin, service := read("private/signer.pem"), read("private/client.pem"), read("controld.pem")
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(service, read("controld.pem")) {
		t.Fatal("idempotent bootstrap replaced service")
	}
	if err := b.Ensure(true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca, read("private/signer.pem")) || !bytes.Equal(admin, read("private/client.pem")) {
		t.Fatal("renewal replaced authority or administrator")
	}
	if bytes.Equal(service, read("controld.pem")) {
		t.Fatal("service not rotated")
	}
	for _, name := range []string{"node.pem", "node.crt", "node.key"} {
		if _, err := os.Stat(filepath.Join(b.Directory, name)); !os.IsNotExist(err) {
			t.Fatalf("bootstrap created Node material: %s", name)
		}
	}
}

func TestBootstrapServiceRenewalPreservesExpiredAdministrator(t *testing.T) {
	b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern"}
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(b.Directory, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	issuer, err := (FileIssuer{BundlePath: filepath.Join(b.Directory, "private/signer.pem"), Cluster: b.Cluster}).Load()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := tls.X509KeyPair(read("client.crt"), read("client.key"))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(admin.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	certificate.NotBefore = time.Now().Add(-48 * time.Hour)
	certificate.NotAfter = time.Now().Add(-24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, certificate, issuer.Certificate, certificate.PublicKey, issuer.Key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := PublishBundle(filepath.Join(b.Directory, "private/client.pem"), certificatePEM, read("client.key")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Directory, "client.crt"), certificatePEM, 0600); err != nil {
		t.Fatal(err)
	}
	preserved := make(map[string][]byte)
	for _, name := range []string{"private/signer.pem", "ca.crt", "private/client.pem", "client.crt", "client.key"} {
		preserved[name] = read(name)
	}
	services := make(map[string][]byte)
	for _, name := range []string{"controld.pem", "gatewayd.pem", "tunneld.pem"} {
		services[name] = read(name)
	}
	if err := b.Ensure(false); err == nil || !strings.Contains(err.Error(), "administrator credential has expired") {
		t.Fatalf("ordinary initialization accepted expired administrator: %v", err)
	}
	if err := b.Ensure(true); err != nil {
		t.Fatalf("expired administrator blocked service renewal: %v", err)
	}
	for name, want := range preserved {
		if !bytes.Equal(want, read(name)) {
			t.Errorf("service renewal changed authority or administrator material: %s", name)
		}
	}
	for name, previous := range services {
		if bytes.Equal(previous, read(name)) {
			t.Errorf("service certificate was not renewed: %s", name)
		}
	}
	if err := b.Ensure(false); err == nil || !strings.Contains(err.Error(), "administrator credential has expired") {
		t.Fatalf("service renewal made the expired administrator acceptable: %v", err)
	}
}

func TestBootstrapServiceRenewalRejectsInvalidAdministrator(t *testing.T) {
	for _, name := range []string{"malformed", "mismatched-key", "foreign-signer", "workload-identity"} {
		t.Run(name, func(t *testing.T) {
			b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern"}
			if err := b.Ensure(false); err != nil {
				t.Fatal(err)
			}
			read := func(path string) []byte {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			var invalid []byte
			switch name {
			case "malformed":
				invalid = []byte("invalid administrator bundle")
			case "mismatched-key":
				invalid = append(read(filepath.Join(b.Directory, "client.crt")), read(filepath.Join(b.Directory, "controld.pem"))...)
			case "foreign-signer":
				foreign := Bootstrap{Directory: t.TempDir(), Cluster: b.Cluster}
				if err := foreign.Ensure(false); err != nil {
					t.Fatal(err)
				}
				invalid = read(filepath.Join(foreign.Directory, "private/client.pem"))
			case "workload-identity":
				invalid = read(filepath.Join(b.Directory, "controld.pem"))
			}
			path := filepath.Join(b.Directory, "private/client.pem")
			if err := os.WriteFile(path, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			if err := b.Ensure(true); err == nil {
				t.Fatal("service renewal accepted invalid administrator material")
			}
			if !bytes.Equal(invalid, read(path)) {
				t.Fatal("service renewal replaced invalid administrator material")
			}
		})
	}
}

func TestBootstrapMissingAuthorityFailsClosed(t *testing.T) {
	for _, name := range []string{"private/signer.pem", "private/client.pem"} {
		t.Run(name, func(t *testing.T) {
			b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern"}
			if err := b.Ensure(false); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(b.Directory, name)); err != nil {
				t.Fatal(err)
			}
			if err := b.Ensure(false); err == nil {
				t.Fatal("missing authority silently regenerated")
			}
		})
	}
}

func TestBootstrapDNSChangeRequiresExplicitRenewal(t *testing.T) {
	b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern", DNSNames: []string{"old.example"}}
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
	b.DNSNames = []string{"new.example"}
	if err := b.Ensure(false); err == nil {
		t.Fatal("uncovered DNS accepted")
	}
	if err := b.Ensure(true); err != nil {
		t.Fatal(err)
	}
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapRejectsConcurrentProvisionerAndRecoversAfterUnlock(t *testing.T) {
	b := Bootstrap{Directory: t.TempDir(), Cluster: "test.axern"}
	lock, err := os.OpenFile(filepath.Join(b.Directory, ".bootstrap.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := b.Ensure(false); err == nil {
		t.Fatal("concurrent authority writer accepted")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := b.Ensure(false); err != nil {
		t.Fatal(err)
	}
}
