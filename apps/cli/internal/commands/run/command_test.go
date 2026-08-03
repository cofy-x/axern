package run

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
)

func TestRunIsForegroundRootAndCreateIsRemoved(t *testing.T) {
	cmd := Command(command.Runtime{})
	for _, name := range []string{"template", "environment", "file", "detach", "wait-timeout"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("run flag --%s is missing", name)
		}
	}
	for _, removed := range []string{"argv", "image-ref", "template-id", "environment-id", "wait", "wait-for"} {
		if cmd.Flags().Lookup(removed) != nil {
			t.Fatalf("removed run flag --%s is still registered", removed)
		}
	}
	if found, _, err := cmd.Find([]string{"create"}); err == nil && found != cmd {
		t.Fatal("run create is still registered")
	}
}

func TestRunRejectsSourceSpecificFlags(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"template version without template", []string{"--environment", "env-1", "--template-version", "v2"}, "--template-version requires --template"},
		{"template registry credential", []string{"--template", "python311", "--registry-credential-id", "credential-1"}, "cannot be combined with --template"},
		{"environment readonly rootfs", []string{"--environment", "env-1", "--rootfs-readonly"}, "require an image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := Command(command.Runtime{})
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRunFileRejectsPositionalDefinition(t *testing.T) {
	cmd := Command(command.Runtime{})
	if err := cmd.Flags().Set("file", "run.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Args(cmd, []string{"python:3.12-slim"}); err == nil {
		t.Fatal("--file accepted a positional image")
	}
}
