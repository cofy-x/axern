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

func TestFunctionSourceCannotEscapeThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "src")); err != nil {
		t.Fatal(err)
	}
	path := writeSpec(t, dir, "function.yaml", `
api_version: axern/v1
kind: Function
metadata: {name: hello}
spec:
  source: {template: python311}
  function:
    runtime: python3.11
    handler: handler.hello
    source: src
`)
	if _, err := Load(path, KindFunction); err == nil || !strings.Contains(err.Error(), "must stay below") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestFunctionDefaultsMatchSDK(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeSpec(t, dir, "function.yaml", `
api_version: axern/v1
kind: Function
metadata: {name: hello}
spec:
  source: {template: python311}
  function: {runtime: python3.11, handler: handler.hello, source: src}
`)
	envelope, err := Load(path, KindFunction)
	if err != nil {
		t.Fatal(err)
	}
	function := envelope.Spec.Function
	if function.TimeoutSeconds != 60 || function.Scaling.MaxReplicas != 1 || function.Scaling.Concurrency != 1 || function.Scaling.IdleTimeout != "5m" {
		t.Fatalf("function defaults = %#v", function)
	}
}

func TestGoldenFunctionSpecMatchesSDKFixture(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "examples", "function-hello", "function.yaml")
	envelope, err := Load(path, KindFunction)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Metadata.Name != "hello" || envelope.Metadata.Namespace != "default" || envelope.Spec.Source.Template != "python311" {
		t.Fatalf("resource metadata or source = %#v", envelope)
	}
	function := envelope.Spec.Function
	if function.Runtime != "python3.11" || function.Handler != "handler.hello" || function.Source != "src" || function.TimeoutSeconds != 600 {
		t.Fatalf("function = %#v", function)
	}
	if function.Scaling.MaxReplicas != 10 || function.Scaling.Concurrency != 2 || function.Scaling.IdleTimeout != "5m" {
		t.Fatalf("scaling = %#v", function.Scaling)
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

func TestFunctionRejectsFieldsThatAreNotPartOfFunctionDeployment(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, field := range map[string]string{
		"replicas":      "  replicas: 1\n",
		"readiness":     "  readiness: {tcp_port: 8080}\n",
		"command-cwd":   "  command: {cwd: /workspace}\n",
		"runtime-class": "  runtime_class: runsc\n",
		"secret-env":    "  secret_env: [{name: TOKEN, secret_id: secret-a, key: token}]\n",
		"image-mount":   "  image_mounts: [{image: example.test/tools:latest, target: /tools}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeSpec(t, dir, name+".yaml", `
api_version: axern/v1
kind: Function
metadata: {name: hello}
spec:
  source: {template: python311}
  function: {runtime: python3.11, handler: handler.hello, source: src}
`+field)
			if _, err := Load(path, KindFunction); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestFunctionSourceMustBeRelative(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "function.yaml", `
api_version: axern/v1
kind: Function
metadata: {name: hello}
spec:
  source: {template: python311}
  function: {runtime: python3.11, handler: handler.hello, source: /tmp/source}
`)
	if _, err := Load(path, KindFunction); err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsNonCanonicalVolumeTargetAndNegativeProbeThreshold(t *testing.T) {
	dir := t.TempDir()
	for name, field := range map[string]string{
		"volume": "  volumes: [{name: data, target: /srv/../data}]\n",
		"probe":  "  readiness: {tcp_port: 8080, failure_threshold: -1}\n",
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
