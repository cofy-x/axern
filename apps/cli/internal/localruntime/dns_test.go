package localruntime

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverLocalDNSNameserversUsesValidatedOverride(t *testing.T) {
	got, err := discoverLocalDNSNameservers(" 192.0.2.53,2001:db8::53,192.0.2.53 ", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.53", "2001:db8::53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nameservers = %#v, want %#v", got, want)
	}
}

func TestDiscoverLocalDNSNameserversRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"not-an-ip", "127.0.0.1", "0.0.0.0", "192.0.2.53,"} {
		t.Run(value, func(t *testing.T) {
			_, err := discoverLocalDNSNameservers(value, nil)
			if err == nil || !strings.Contains(err.Error(), localDNSNameserversEnv) {
				t.Fatalf("discoverLocalDNSNameservers(%q) error = %v", value, err)
			}
		})
	}
}

func TestDiscoverLocalDNSNameserversFallsBackPastStubResolver(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub-resolv.conf")
	uplink := filepath.Join(dir, "uplink-resolv.conf")
	if err := os.WriteFile(stub, []byte("nameserver 127.0.0.53\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uplink, []byte("nameserver 10.20.30.40\nnameserver 10.20.30.40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := discoverLocalDNSNameservers("", []string{stub, uplink})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"10.20.30.40"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nameservers = %#v, want %#v", got, want)
	}
}

func TestDiscoverLocalDNSNameserversRequiresReachableResolver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 127.0.0.11\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverLocalDNSNameservers("", []string{path}); err == nil {
		t.Fatal("discoverLocalDNSNameservers() error = nil")
	}
}

func TestDiscoverLocalDNSNameserversIgnoresPartiallyReadResolverFile(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken-resolv.conf")
	fallback := filepath.Join(dir, "fallback-resolv.conf")
	contents := "nameserver 192.0.2.53\n" + strings.Repeat("x", 64*1024+1)
	if err := os.WriteFile(broken, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("nameserver 198.51.100.53\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := discoverLocalDNSNameservers("", []string{broken, fallback})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"198.51.100.53"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nameservers = %#v, want %#v", got, want)
	}
}
