package localruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/localbundle"
	gossh "golang.org/x/crypto/ssh"
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

func TestLocalBundleLimitsExternalRegistryNetworkToRegistryConsumers(t *testing.T) {
	compose := string(localbundle.Compose)
	for _, contract := range []string{
		"name: axern-local-registry",
		"controld:",
		"node:",
		"networks: [default, registry-access]",
	} {
		if !strings.Contains(compose, contract) {
			t.Fatalf("embedded Compose is missing %q", contract)
		}
	}
	if got := strings.Count(compose, "networks: [default, registry-access]"); got != 2 {
		t.Fatalf("registry network consumer count = %d, want 2", got)
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
	data, err := os.ReadFile(filepath.Join(certs, "gatewayd.pem"))
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
	for _, path := range []string{filepath.Join(certs, "client.key"), filepath.Join(ssh, "gateway_client_ed25519")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, got)
		}
	}
}

func TestValidSSHPrivateKeyAcceptsOpenSSHEd25519(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if !validSSHPrivateKey(path) {
		t.Fatal("OpenSSH Ed25519 private key was rejected")
	}
}

func TestCertificateSetRejectsLostSigningAuthority(t *testing.T) {
	dir := t.TempDir()
	if err := ensurePKI(dir); err != nil {
		t.Fatal(err)
	}
	if !validCertificateSet(dir) {
		t.Fatal("new certificate set rejected")
	}
	if err := os.Remove(filepath.Join(dir, "private", "signer.pem")); err != nil {
		t.Fatal(err)
	}
	if validCertificateSet(dir) {
		t.Fatal("missing authority accepted")
	}
	if err := ensurePKI(dir); err == nil {
		t.Fatal("lost authority silently replaced")
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

func TestWriteEnvGeneratesAndRepairsSecretsMasterKey(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "192.0.2.53")
	tests := []struct {
		name       string
		existing   string
		wantSame   bool
		wantBase64 bool
	}{
		{name: "new key", wantBase64: true},
		{name: "invalid failed initialization key", existing: "0123456789abcdef0123456789abcdef0123456789abcdef", wantBase64: true},
		{name: "valid raw key", existing: "0123456789abcdef0123456789abcdef", wantSame: true},
		{name: "valid base64 key", existing: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), wantSame: true, wantBase64: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			manager := &Manager{Dir: dir}
			if test.existing != "" {
				data, err := json.Marshal(map[string]string{"master": test.existing})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "secrets.json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.writeEnv("", []string{"forge-seed-registry:5000"}); err != nil {
				t.Fatal(err)
			}
			envData, err := os.ReadFile(filepath.Join(dir, "compose.env"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(envData, []byte(`AXNODED_DNS_NAMESERVERS="192.0.2.53"`)) {
				t.Fatalf("compose env does not contain resolved workload DNS: %q", envData)
			}
			for _, contract := range []string{`REGISTRY_IMAGE="registry:2"`, `CONTROLD_INSECURE_REGISTRIES="registry:5000,forge-seed-registry:5000"`, `CONTAINER_NO_PROXY="localhost,127.0.0.1,::1,host.docker.internal,controld,gatewayd,tunneld,node,postgres,registry,.svc,.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,forge-seed-registry"`} {
				if !bytes.Contains(envData, []byte(contract)) {
					t.Fatalf("compose env does not contain %s: %q", contract, envData)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
			if err != nil {
				t.Fatal(err)
			}
			var secrets map[string]string
			if err := json.Unmarshal(data, &secrets); err != nil {
				t.Fatal(err)
			}
			key := secrets["master"]
			if !validSecretsMasterKey(key) {
				t.Fatalf("generated master key is invalid: %q", key)
			}
			if test.wantSame && key != test.existing {
				t.Fatalf("valid master key changed: got %q, want %q", key, test.existing)
			}
			if test.wantBase64 {
				decoded, err := base64.StdEncoding.DecodeString(key)
				if err != nil || len(decoded) != 32 {
					t.Fatalf("master key is not base64-encoded 32 bytes: length=%d err=%v", len(decoded), err)
				}
			}
		})
	}
}

func TestDoctorReportsRequiredRuntimeDNSFailure(t *testing.T) {
	t.Setenv(localDNSNameserversEnv, "127.0.0.1")
	manager := &Manager{Dir: t.TempDir(), Runner: &recordingRunner{}, Stdout: io.Discard, Stderr: io.Discard}
	report := manager.doctor(context.Background(), false, DoctorOptions{}, doctorDNSConfigDesired)
	for _, check := range report.Checks {
		if check.Name != "runtime_dns_config" {
			continue
		}
		if check.Status != checkFail || check.Code != "runtime_dns_config_invalid" || !strings.Contains(check.Remediation, localDNSNameserversEnv) {
			t.Fatalf("runtime DNS check = %#v", check)
		}
		return
	}
	t.Fatal("doctor did not report runtime_dns")
}

type recordingRunner struct {
	calls  [][]string
	runErr error
}

func (r *recordingRunner) Run(_ context.Context, _, _ io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.runErr
}

func (*recordingRunner) Output(context.Context, string, ...string) ([]byte, error) { return nil, nil }

func TestStartupDiagnosticsIncludesBoundedCoreLogs(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	manager := &Manager{Dir: t.TempDir(), Runner: runner, Stdout: io.Discard, Stderr: &stderr}
	manager.printStartupDiagnostics("")
	if len(runner.calls) != 2 {
		t.Fatalf("diagnostic calls = %d, want 2", len(runner.calls))
	}
	if got, want := runner.calls[1][len(runner.calls[1])-9:], []string{"logs", "--no-color", "--tail", "80", "registry", "controld", "tunneld", "node", "gatewayd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("log diagnostics = %#v, want %#v", got, want)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Recent core service logs follow.")) {
		t.Fatalf("diagnostic stderr = %q", stderr.String())
	}
}

func TestPlatformImagePullUsesBoundedProgressOutsideTerminal(t *testing.T) {
	runner := &recordingRunner{}
	var stderr bytes.Buffer
	manager := &Manager{Dir: t.TempDir(), Runner: runner, Stdout: io.Discard, Stderr: &stderr}
	if err := manager.pullPlatformImages(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("pull calls = %d, want 1", len(runner.calls))
	}
	wantTail := []string{"--progress", "quiet", "pull", "registry", "postgres", "controld", "tunneld", "node", "gatewayd"}
	got := runner.calls[0]
	if len(got) < len(wantTail) || !reflect.DeepEqual(got[len(got)-len(wantTail):], wantTail) {
		t.Fatalf("pull command = %#v, want tail %#v", got, wantTail)
	}
	for _, message := range []string{"Pulling Axern platform images...", "Axern platform images are ready (elapsed"} {
		if !strings.Contains(stderr.String(), message) {
			t.Fatalf("pull stderr = %q, missing %q", stderr.String(), message)
		}
	}
}

func TestComposePullArgsPreserveInteractiveProgress(t *testing.T) {
	services := []string{"postgres", "node"}
	if got, want := composePullArgs(true, services), []string{"pull", "postgres", "node"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interactive args = %#v, want %#v", got, want)
	}
	if got, want := composePullArgs(false, services), []string{"--progress", "quiet", "pull", "postgres", "node"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("non-interactive args = %#v, want %#v", got, want)
	}
}

func TestPlatformImagePullPreservesFailureWithoutSuccessMessage(t *testing.T) {
	runner := &recordingRunner{runErr: errors.New("registry unavailable")}
	var stderr bytes.Buffer
	manager := &Manager{Dir: t.TempDir(), Runner: runner, Stdout: io.Discard, Stderr: &stderr}
	err := manager.pullPlatformImages(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "registry unavailable") {
		t.Fatalf("pull error = %v", err)
	}
	if strings.Contains(stderr.String(), "images are ready") {
		t.Fatalf("pull stderr announced success: %q", stderr.String())
	}
}
