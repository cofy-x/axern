package resourcespec

import (
	axernsdk "github.com/cofy-x/axern/sdk/go"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkPolicySpecUsesPublicSDKContract(t *testing.T) {
	dns, err := axernsdk.DenyDNSNetworkPolicy("DENIED.example.", "denied.example")
	if err != nil {
		t.Fatal(err)
	}
	strict, err := axernsdk.NewStrictNetworkPolicy([]string{"EXAMPLE.COM."}, []axernsdk.CIDRRule{{CIDR: "198.19.0.7/24", Protocol: axernsdk.EgressProtocolTCP, Ports: []axernsdk.PortRange{{Start: 18080}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, policy string
		want         *axernsdk.NetworkPolicy
	}{
		{"dns", `{"dns_deny":{"denied_domains":["DENIED.example.","denied.example"]}}`, dns},
		{"strict", `{"strict":{"allowed_domains":["EXAMPLE.COM."],"allowed_cidrs":[{"cidr":"198.19.0.7/24","protocol":"tcp","ports":[{"start":18080}]}]}}`, strict},
		{"isolated", `{"strict":{}}`, axernsdk.DenyAllNetworkPolicy()},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeSpec(t, t.TempDir(), "run.json", `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"image":"example.test/image:latest"},"network_policy":`+test.policy+`}}`)
			envelope, err := Load(path, KindRun)
			if err != nil {
				t.Fatal(err)
			}
			config, err := envelope.ExecutionConfig()
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(config.GetNetwork().GetEgressPolicy(), test.want.Proto()) {
				t.Fatalf("policy = %v", config.GetNetwork())
			}
		})
	}
	config, err := (Envelope{}).ExecutionConfig()
	if err != nil || config.GetNetwork() != nil {
		t.Fatalf("absent policy: %v, %v", config, err)
	}
}

func TestNetworkPolicySpecRejectsAmbiguityAndInvalidGrants(t *testing.T) {
	for _, policy := range []string{
		`{}`, `{"strict":{},"dns_deny":{"denied_domains":["example.com"]}}`,
		`{"dns_deny":{"denied_domains":[]}}`, `{"strict":{"allowed_domains":["https://example.com"]}}`,
		`{"strict":{"allowed_cidrs":[{"cidr":"127.0.0.1/32","protocol":"tcp","ports":[{"start":80}]}]}}`,
		`{"strict":{"allowed_cidrs":[{"cidr":"198.19.0.2/32","protocol":"sctp","ports":[{"start":80}]}]}}`,
		`{"strict":{"allowed_cidrs":[{"cidr":"198.19.0.2/32","protocol":"tcp","ports":[{"start":90,"end":80}]}]}}`,
		`{"strict":{"obsolete":true}}`,
	} {
		path := writeSpec(t, t.TempDir(), "run.json", `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"image":"example.test/image:latest"},"network_policy":`+policy+`}}`)
		if _, err := Load(path, KindRun); err == nil {
			t.Fatalf("accepted %s", policy)
		}
	}
}

func TestLoadRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"unknown.yaml": `api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {image: example.test/runtime:latest}
  obsolete: true
`,
		"multiple.yaml": `api_version: axern/v1
kind: Run
metadata: {}
spec: {source: {image: example.test/runtime:latest}}
---
api_version: axern/v1
kind: Run
metadata: {}
spec: {source: {image: example.test/runtime:latest}}
`,
		"multiple.json": `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"image":"example.test/runtime:latest"}}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeSpec(t, dir, name, content), KindRun)
			if err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadRejectsConflictingSourcesAndKindMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "run.yaml", `
api_version: axern/v1
kind: Run
metadata: {}
spec:
  source:
    image: example.test/image:latest
    environment: env-1
`)
	if _, err := Load(path, KindRun); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("source conflict error = %v", err)
	}
	path = writeSpec(t, dir, "sandbox.yaml", `
api_version: axern/v1
kind: Sandbox
metadata: {}
spec: {source: {image: example.test/runtime:latest}}
`)
	if _, err := Load(path, KindRun); err == nil || !strings.Contains(err.Error(), `kind must be "Run"`) {
		t.Fatalf("kind mismatch error = %v", err)
	}
}

func TestExecutionConfigMapsStructuredSecretAndImageMounts(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "run.yaml", `
api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {image: example.test/runtime:v2}
  secret_env:
    - {name: TOKEN, secret_id: secret-a, key: token}
  secret_files:
    - {path: /run/secrets/config, secret_id: secret-a, key: config, mode: "0440"}
  image_mounts:
    - {image: example.test/tools:latest, target: /opt/tools}
  rootfs_snapshot: true
`)
	envelope, err := Load(path, KindRun)
	if err != nil {
		t.Fatal(err)
	}
	_, environment := envelope.EnvironmentSpec()
	if got := environment.GetImage().GetRef(); got != "example.test/runtime:v2" {
		t.Fatalf("image ref = %q", got)
	}
	config, err := envelope.ExecutionConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.GetSecretEnv()[0].GetSecretID() != "secret-a" || config.GetSecretFiles()[0].GetMode() != 0o440 || config.GetImageMounts()[0].GetTarget() != "/opt/tools" {
		t.Fatalf("execution config = %#v", config)
	}
	if config.GetRootfsSnapshot() == nil {
		t.Fatal("rootfs snapshot contract was not mapped")
	}
}

func TestLoadRejectsSourceOptionsForWrongSourceKind(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "run.yaml", `
api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {environment: env-1, registry_credential_id: secret-a}
`)
	if _, err := Load(path, KindRun); err == nil || !strings.Contains(err.Error(), "require image") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsNonCanonicalImageMountTarget(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "image-mount.yaml", `
api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {image: example.test/runtime:latest}
  image_mounts: [{image: tools:latest, target: /srv/../data}]
`)
	if _, err := Load(path, KindRun); err == nil {
		t.Fatal("Load() error = nil")
	}
}

func writeSpec(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkspaceSelectionUsesDataIdentityAndRejectsMountPaths(t *testing.T) {
	path := writeSpec(t, t.TempDir(), "run.json", `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"image":"example.test/runtime:latest"},"workspace_id":"ws-retained"}}`)
	value, err := Load(path, KindRun)
	if err != nil || value.Spec.WorkspaceID != "ws-retained" {
		t.Fatalf("Workspace intent lost: %v, %v", value, err)
	}
	path = writeSpec(t, t.TempDir(), "run.json", `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"image":"example.test/runtime:latest"},"workspace_id":"ws-retained","workspace_path":"/host/data"}}`)
	if _, err := Load(path, KindRun); err == nil {
		t.Fatal("host mount selector accepted")
	}
}
