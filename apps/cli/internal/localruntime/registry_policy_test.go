package localruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeExternalInsecureRegistries(t *testing.T) {
	got, err := normalizeExternalInsecureRegistries([]string{
		"Registry.Example:05000",
		"forge-seed-registry:5000",
		"registry.example:5000",
		"[2001:db8::1]:5000",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"[2001:db8::1]:5000", "forge-seed-registry:5000", "registry.example:5000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized registries = %#v, want %#v", got, want)
	}
}

func TestResolveExternalRegistryPolicyPresenceSemantics(t *testing.T) {
	existing := []string{"old.example:5000"}
	tests := []struct {
		name    string
		exists  bool
		options UpOptions
		want    []string
		wantErr bool
	}{
		{name: "omitted preserves", exists: true, want: existing},
		{name: "omitted on new stack is empty"},
		{name: "set replaces", exists: true, options: UpOptions{SetInsecureRegistries: true, ExternalInsecureRegistries: []string{"new.example:5000"}}, want: []string{"new.example:5000"}},
		{name: "clear removes", exists: true, options: UpOptions{ClearInsecureRegistries: true}},
		{name: "set and clear conflict", exists: true, options: UpOptions{SetInsecureRegistries: true, ClearInsecureRegistries: true}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveExternalRegistryPolicy(existing, test.exists, test.options)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, test.wantErr)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("policy = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestNormalizeExternalInsecureRegistriesRejectsNonHosts(t *testing.T) {
	for _, value := range []string{
		"http://registry.example:5000",
		"registry.example:5000/team/image",
		"user@registry.example:5000",
		"*.example:5000",
		"registry.example:0",
		"registry.example:65536",
		"forge-seed-registry",
		"[registry.example]:5000",
		"[127.0.0.1]:5000",
		" registry.example:5000",
		internalRegistryHost,
	} {
		t.Run(strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			if _, err := normalizeExternalInsecureRegistries([]string{value}); err == nil {
				t.Fatalf("normalizeExternalInsecureRegistries(%q) error = nil", value)
			}
		})
	}
}

func TestReadMaterializedInsecureRegistries(t *testing.T) {
	path := t.TempDir() + "/compose.env"
	write := func(value string) {
		t.Helper()
		if err := writeAtomic(path, []byte("CONTROLD_INSECURE_REGISTRIES=\""+value+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("registry:5000,forge-seed-registry:5000")
	got, err := readMaterializedInsecureRegistries(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"forge-seed-registry:5000"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized registries = %#v, want %#v", got, want)
	}

	write("forge-seed-registry:5000")
	if _, err := readMaterializedInsecureRegistries(path); err == nil {
		t.Fatal("missing internal registry policy was accepted")
	}
}

func TestRegistryNoProxyHosts(t *testing.T) {
	got := registryNoProxyHosts([]string{"registry.example:5000", "registry.example:6000", "[2001:db8::1]:5000"})
	want := []string{"registry.example", "2001:db8::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-proxy hosts = %#v, want %#v", got, want)
	}
}

func TestLoadMetadataNormalizesAndRejectsRegistryPolicy(t *testing.T) {
	path := t.TempDir() + "/metadata.json"
	if err := saveMetadata(path, Metadata{Version: "dev", ExternalInsecureRegistries: []string{"REGISTRY.EXAMPLE:05000", "registry.example:5000"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	metadata, err := loadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"registry.example:5000"}; !reflect.DeepEqual(metadata.ExternalInsecureRegistries, want) {
		t.Fatalf("metadata registries = %#v, want %#v", metadata.ExternalInsecureRegistries, want)
	}
	if err := os.WriteFile(path, []byte(`{"version":"dev","external_insecure_registries":["http://registry.example:5000"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMetadata(path); err == nil {
		t.Fatal("invalid persisted registry policy was accepted")
	}
}

func TestEnsureRegistryNetworkCreatesMissingNetwork(t *testing.T) {
	runner := &networkRunner{outputs: []networkOutput{{}}}
	manager := &Manager{Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	if err := manager.ensureRegistryNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "network", "create", "--driver", "bridge", "--label", "io.axern.local.registry=true", RegistryNetworkName}
	if !reflect.DeepEqual(runner.runCall, want) {
		t.Fatalf("network creation = %#v, want %#v", runner.runCall, want)
	}
}

func TestEnsureRegistryNetworkReusesExistingNetwork(t *testing.T) {
	runner := &networkRunner{outputs: []networkOutput{{data: []byte(RegistryNetworkName + "\n")}, {data: []byte("bridge|local|true\n")}}}
	manager := &Manager{Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	if err := manager.ensureRegistryNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.runCall != nil {
		t.Fatalf("existing network was recreated: %#v", runner.runCall)
	}
}

func TestEnsureRegistryNetworkRejectsConflictingNetwork(t *testing.T) {
	runner := &networkRunner{outputs: []networkOutput{{data: []byte(RegistryNetworkName + "\n")}, {data: []byte("overlay|swarm|\n")}}}
	manager := &Manager{Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	if err := manager.ensureRegistryNetwork(context.Background()); err == nil || !strings.Contains(err.Error(), "must be an Axern-managed local bridge network") {
		t.Fatalf("conflicting network error = %v", err)
	}
	if runner.runCall != nil {
		t.Fatalf("conflicting network was replaced: %#v", runner.runCall)
	}
}

func TestEnsureRegistryNetworkPropagatesDockerFailure(t *testing.T) {
	runner := &networkRunner{outputs: []networkOutput{{err: errors.New("daemon unavailable")}}}
	manager := &Manager{Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	err := manager.ensureRegistryNetwork(context.Background())
	if err == nil || !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatalf("ensureRegistryNetwork() error = %v", err)
	}
	if runner.runCall != nil {
		t.Fatalf("network creation was attempted after an inspection failure: %#v", runner.runCall)
	}
}

type networkOutput struct {
	data []byte
	err  error
}

type networkRunner struct {
	outputs []networkOutput
	runCall []string
}

func (r *networkRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if len(r.outputs) == 0 {
		return nil, errors.New("unexpected output call")
	}
	value := r.outputs[0]
	r.outputs = r.outputs[1:]
	return value.data, value.err
}

func (r *networkRunner) Run(_ context.Context, _, _ io.Writer, name string, args ...string) error {
	r.runCall = append([]string{name}, args...)
	return nil
}
