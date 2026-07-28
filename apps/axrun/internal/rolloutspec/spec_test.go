package rolloutspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndRejectUnsupportedAPIVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout.yaml")
	specYAML := `api_version: axrun/v1
kind: Rollout
spec:
  task_set:
    ref: bundle
  agent:
    name: codex
    runtime:
      kind: agent_image
      image: example.invalid/codex@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    profile: production
    approval_policy: never
  model: openai/gpt-5
  execution:
    runner: axern
    concurrency: 32
    attempts: 4
`
	if err := os.WriteFile(path, []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if spec.APIVersion != "axrun/v1" || spec.Spec.Execution.Concurrency != 32 {
		t.Fatalf("spec = %#v", spec)
	}

	unsupported := filepath.Join(root, "unsupported.yaml")
	if err := os.WriteFile(unsupported, []byte("api_version: axrun/unsupported\nkind: Rollout\nspec: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unsupported); err == nil {
		t.Fatal("unsupported rollout API version was accepted")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.yaml")
	data := "api_version: axrun/v1\nkind: Rollout\nspec:\n  input: old\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("old input field was accepted")
	}
}
