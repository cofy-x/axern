package cliapp

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/spf13/cobra"
)

func TestCommandTreeUsesCanonicalProductCommands(t *testing.T) {
	root := New("test")
	want := map[string][]string{"context": {"ctx"}, "namespace": {"ns"}}
	for name, aliases := range want {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Fatalf("missing command %s: %v", name, err)
		}
		if strings.Join(cmd.Aliases, ",") != strings.Join(aliases, ",") {
			t.Fatalf("%s aliases=%v, want %v", name, cmd.Aliases, aliases)
		}
	}
	for _, removed := range [][]string{{"invoke"}, {"run", "lease"}, {"service"}, {"agent"}, {"quota", "describe"}} {
		cmd, args, _ := root.Find(removed)
		if cmd != root && len(args) == 0 {
			t.Fatalf("removed command is still registered: %v", removed)
		}
	}
}

func TestHelpIncludesCoreSandboxCommands(t *testing.T) {
	root := New("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, value := range []string{"completion", "doctor", "run", "ssh", "tunnel"} {
		if !strings.Contains(text, value) {
			t.Fatalf("help missing %s:\n%s", value, text)
		}
	}
}

func TestExecuteClassifiesUnknownCommandAsUsageError(t *testing.T) {
	err := Execute(New("test"), []string{"obsolete-command"})
	var usage command.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Execute() error = %v, want UsageError", err)
	}
}

func TestVersionCommandPrintsBareVersion(t *testing.T) {
	root := New("1.2.3")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "1.2.3\n" {
		t.Fatalf("version output = %q", got)
	}
}

func TestCommandTreeHasNoInheritedFlagConflicts(t *testing.T) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		cmd.InheritedFlags()
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(New("test"))
}
