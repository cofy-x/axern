package contextcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/config"
)

func TestImportKubernetesCommandPassesExplicitClusterSelection(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	kubectlPath := filepath.Join(dir, "kubectl")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$AXERN_TEST_KUBECTL_ARGS"
printf '%s\n' '{"data":{"ca.crt":"Y2E=","client.crt":"Y2VydA==","client.key":"a2V5"}}'
`
	if err := os.WriteFile(kubectlPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AXERN_TEST_KUBECTL_ARGS", argsPath)

	configPath := filepath.Join(dir, "config.json")
	runtime := command.Runtime{Options: &command.Options{ConfigPath: configPath, Output: "table"}}
	cmd := Command(runtime)
	cmd.SetArgs([]string{
		"import-kubernetes", "kind", "--namespace", "sandbox-system",
		"--secret", "sandbox-pki", "--kubeconfig", "/tmp/kubeconfig",
		"--kube-context", "kind-axern", "--cert-dir", filepath.Join(dir, "certs"),
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	actualArgs, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "--kubeconfig\n/tmp/kubeconfig\n--context\nkind-axern\n--namespace\nsandbox-system\nget\nsecret\nsandbox-pki\n--output\njson\n"
	if string(actualArgs) != wantArgs {
		t.Fatalf("kubectl args = %q, want %q", actualArgs, wantArgs)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "kind" || !strings.HasSuffix(cfg.Contexts["kind"].TLS.Key, "client.key") {
		t.Fatalf("unexpected imported context: %+v", cfg)
	}
}
