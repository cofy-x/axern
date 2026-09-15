package workloadtls

import (
	"bytes"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
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
