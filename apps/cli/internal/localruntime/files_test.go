package localruntime

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirHonorsAxernHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AXERN_HOME", root)
	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "local"); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}

func TestGeneratedIdentityFilesAreValidAndPrivate(t *testing.T) {
	dir := t.TempDir()
	certs := filepath.Join(dir, "certs")
	ssh := filepath.Join(dir, "ssh")
	if err := ensurePKI(certs); err != nil {
		t.Fatal(err)
	}
	if err := ensureSSH(ssh); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(certs, "gatewayd.crt"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) == 0 || cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Fatalf("gateway certificate does not cover loopback: %v", cert.IPAddresses)
	}
	for _, path := range []string{filepath.Join(certs, "client.key"), filepath.Join(ssh, "gateway_client_ed25519"), filepath.Join(ssh, "authorized_keys")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, got)
		}
	}
}

func TestVersionLess(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        bool
	}{{"1.2.3", "1.2.4", true}, {"1.10.0", "1.9.9", false}, {"2.0.0-rc.1", "2.0.0", true}, {"2.0.0", "2.0.0-rc.1", false}, {"2.0.0-rc.2", "2.0.0-rc.10", true}} {
		if got := versionLess(test.left, test.right); got != test.want {
			t.Fatalf("versionLess(%q, %q) = %v, want %v", test.left, test.right, got, test.want)
		}
	}
}

func TestSupportedUpgrade(t *testing.T) {
	for _, test := range []struct {
		from, to string
		want     bool
	}{
		{"0.2.9", "0.3.0", true},
		{"0.1.0", "0.3.0", false},
		{"0.3.0", "0.3.2", true},
		{"1.4.0", "1.7.0", true},
		{"1.4.0", "2.0.0", false},
	} {
		if got := supportedUpgrade(test.from, test.to); got != test.want {
			t.Errorf("supportedUpgrade(%q, %q) = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestContainerProxyOnlyRewritesLoopbackHost(t *testing.T) {
	tests := map[string]string{
		"http://localhost:3128":                     "http://host.docker.internal:3128",
		"http://127.0.0.1:3128/path?next=localhost": "http://host.docker.internal:3128/path?next=localhost",
		"http://[::1]:3128":                         "http://host.docker.internal:3128",
		"http://proxy.example/localhost":            "http://proxy.example/localhost",
		"not a url mentioning localhost":            "not a url mentioning localhost",
	}
	for input, want := range tests {
		if got := containerProxy(input); got != want {
			t.Errorf("containerProxy(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQuoteDotEnv(t *testing.T) {
	if got, want := quoteDotEnv("a b#c\\d\n"), `"a b#c\\d\n"`; got != want {
		t.Fatalf("quoteDotEnv() = %q, want %q", got, want)
	}
}
