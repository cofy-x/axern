package sshcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestResolveConnectionExplicitTransportDoesNotReadContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"control_target":"obsolete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXERN_SSH_ENDPOINT", "gateway:22")
	t.Setenv("AXERN_SSH_IDENTITY_FILE", "/keys/client")
	runtime := command.Runtime{Options: &command.Options{ConfigPath: path}}
	cmd := Command(runtime)
	opts := &options{}

	if err := resolveConnection(runtime, cmd, opts); err != nil {
		t.Fatal(err)
	}
	if opts.endpoint != "gateway:22" || opts.identityFile != "/keys/client" {
		t.Fatalf("resolveConnection() = %+v", opts)
	}
}
