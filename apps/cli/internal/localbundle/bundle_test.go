package localbundle

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEmbeddedBundleIsSelfContainedAndLoopbackOnly(t *testing.T) {
	for _, value := range [][]byte{Compose, CollectorConfig} {
		if len(bytes.TrimSpace(value)) == 0 {
			t.Fatal("embedded local asset is empty")
		}
	}
	if bytes.Contains(Compose, []byte(":latest")) {
		t.Fatal("local bundle contains a floating latest image")
	}
	if !bytes.Contains(Compose, []byte("AXNODED_DNS_NAMESERVERS: ${AXNODED_DNS_NAMESERVERS}")) {
		t.Fatal("local bundle does not pass resolved workload DNS to axnoded")
	}
	for _, contract := range []string{
		"AXNODED_CGROUP_ENFORCEMENT: disabled_dev",
		"AXNODED_MEMORY_SYSTEM_RESERVE_BYTES: \"0\"",
		"AXNODED_ROOTFS_SNAPSHOT_REPOSITORY: registry:5000/axern/rootfs-snapshots",
		"registry: {condition: service_healthy}",
		"snapshot-registry-data:/var/lib/registry",
		"CONTROLD_INSECURE_REGISTRIES: ${CONTROLD_INSECURE_REGISTRIES}",
		"IMAGEMGR_INSECURE_REGISTRIES: ${CONTROLD_INSECURE_REGISTRIES}",
	} {
		if !bytes.Contains(Compose, []byte(contract)) {
			t.Fatalf("local bundle is missing the local cgroup contract %q", contract)
		}
	}
	for _, port := range []string{"POSTGRES_PORT", "CONTROLD_HTTP_PORT", "GATEWAY_CONTROL_PORT", "GATEWAY_HTTP_PORT", "GATEWAY_SSH_PORT", "OTEL_GRPC_PORT", "OTEL_HTTP_PORT", "LGTM_UI_PORT"} {
		mapping := []byte("127.0.0.1:${" + port + "}:")
		if !bytes.Contains(Compose, mapping) {
			t.Fatalf("host port %s is not bound explicitly to loopback", port)
		}
	}
}

func TestEmbeddedComposeRegistryNetwork(t *testing.T) {
	var document struct {
		Networks map[string]struct {
			External bool   `yaml:"external"`
			Name     string `yaml:"name"`
		} `yaml:"networks"`
		Services map[string]struct {
			Networks []string `yaml:"networks"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(Compose, &document); err != nil {
		t.Fatalf("parse embedded Compose: %v", err)
	}
	network := document.Networks["registry-access"]
	if !network.External || network.Name != "axern-local-registry" {
		t.Fatalf("registry network = %+v", network)
	}
	for service, value := range document.Services {
		wantRegistryAccess := service == "controld" || service == "node"
		hasRegistryAccess := false
		for _, network := range value.Networks {
			hasRegistryAccess = hasRegistryAccess || network == "registry-access"
		}
		if hasRegistryAccess != wantRegistryAccess {
			t.Fatalf("service %s registry-access = %t, want %t", service, hasRegistryAccess, wantRegistryAccess)
		}
	}
}

func TestReleaseImageLockOverridesEveryImage(t *testing.T) {
	previous := imageLock
	t.Cleanup(func() { imageLock = previous })
	defaults := ImageReferences("1.2.3")
	entries := make([]string, 0, len(defaults))
	for key := range defaults {
		entries = append(entries, key+"=registry.example/"+key+"@sha256:"+strings.Repeat("a", 64))
	}
	imageLock = strings.Join(entries, ";")
	for key, value := range ImageReferences("1.2.3") {
		if !strings.Contains(value, "@sha256:") {
			t.Fatalf("%s is not digest locked: %s", key, value)
		}
	}
}
