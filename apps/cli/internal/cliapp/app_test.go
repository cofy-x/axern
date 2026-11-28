package cliapp

import (
	"bytes"
	"errors"
	"slices"
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

func TestExecutePreservesRunArgumentsAfterDelimiter(t *testing.T) {
	root := New("test")
	run, _, err := root.Find([]string{"run"})
	if err != nil || run.Name() != "run" {
		t.Fatalf("resolve run: %v", err)
	}
	called := false
	run.RunE = func(cmd *cobra.Command, args []string) error {
		called = true
		want := []string{"python:3.12", "echo", "--help"}
		if !slices.Equal(args, want) || cmd.ArgsLenAtDash() != 1 {
			t.Fatalf("run args = %q, delimiter index = %d", args, cmd.ArgsLenAtDash())
		}
		format, err := cmd.Flags().GetString("output")
		if err != nil || format != "json" {
			t.Fatalf("run output flag = %q, error = %v", format, err)
		}
		return nil
	}
	if err := Execute(root, []string{"run", "-o", "json", "python:3.12", "--", "echo", "--help"}); err != nil || !called {
		t.Fatalf("Execute(run) called=%t, error=%v", called, err)
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

func TestCommandTreeSeparatesServingAndDevelopment(t *testing.T) {
	root := New("test")
	for _, name := range []string{"server", "dev", "workspace"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == root {
			t.Fatalf("missing %s command: %v", name, err)
		}
	}
	if cmd, _, _ := root.Find([]string{"local"}); cmd != root {
		t.Fatal("obsolete local command remains")
	}
}
