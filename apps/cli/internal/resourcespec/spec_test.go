package resourcespec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceReplicasDistinguishesOmittedAndExplicitZero(t *testing.T) {
	dir := t.TempDir()
	omitted := writeSpec(t, dir, "omitted.yaml", `
api_version: axern/v1
kind: Service
metadata: {namespace: default}
spec:
  source: {template: python311}
`)
	explicitZero := writeSpec(t, dir, "zero.json", `{
  "api_version": "axern/v1",
  "kind": "Service",
  "metadata": {"namespace": "default"},
  "spec": {"source": {"template": "python311"}, "replicas": 0}
}`)

	first, err := Load(omitted, KindService)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(explicitZero, KindService)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.ServiceReplicas(); got != 1 {
		t.Fatalf("omitted replicas = %d, want 1", got)
	}
	if got := second.ServiceReplicas(); got != 0 {
		t.Fatalf("explicit replicas = %d, want 0", got)
	}
}

func TestLoadRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"unknown.yaml": `api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {template: python311}
  obsolete: true
`,
		"multiple.yaml": `api_version: axern/v1
kind: Run
metadata: {}
spec: {source: {template: python311}}
---
api_version: axern/v1
kind: Run
metadata: {}
spec: {source: {template: python311}}
`,
		"multiple.json": `{"api_version":"axern/v1","kind":"Run","metadata":{},"spec":{"source":{"template":"python311"}}} {}`,
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
    template: python311
    image: example.test/image:latest
`)
	if _, err := Load(path, KindRun); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("source conflict error = %v", err)
	}
	path = writeSpec(t, dir, "service.yaml", `
api_version: axern/v1
kind: Service
metadata: {}
spec: {source: {template: python311}}
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
  source: {template: python311, template_version: v2}
  secret_env:
    - {name: TOKEN, secret_id: secret-a, key: token}
  secret_files:
    - {path: /run/secrets/config, secret_id: secret-a, key: config, mode: "0440"}
  image_mounts:
    - {image: example.test/tools:latest, target: /opt/tools}
`)
	envelope, err := Load(path, KindRun)
	if err != nil {
		t.Fatal(err)
	}
	_, environment := envelope.EnvironmentSpec()
	if environment.GetTemplateVersion() != "v2" {
		t.Fatalf("template version = %q", environment.GetTemplateVersion())
	}
	config, err := envelope.ExecutionConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.GetSecretEnv()[0].GetSecretID() != "secret-a" || config.GetSecretFiles()[0].GetMode() != 0o440 || !config.GetImageMounts()[0].GetReadonly() {
		t.Fatalf("execution config = %#v", config)
	}
}

func TestLoadRejectsSourceOptionsForWrongSourceKind(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "run.yaml", `
api_version: axern/v1
kind: Run
metadata: {}
spec:
  source: {template: python311, registry_credential_id: secret-a}
`)
	if _, err := Load(path, KindRun); err == nil || !strings.Contains(err.Error(), "require image") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsNonCanonicalImageMountTargetAndNegativeProbeThreshold(t *testing.T) {
	dir := t.TempDir()
	for name, field := range map[string]string{
		"image-mount": "  image_mounts: [{image: tools:latest, target: /srv/../data}]\n",
		"probe":       "  readiness: {tcp_port: 8080, failure_threshold: -1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeSpec(t, dir, name+".yaml", `
api_version: axern/v1
kind: Service
metadata: {}
spec:
  source: {template: python311}
`+field)
			if _, err := Load(path, KindService); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
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
