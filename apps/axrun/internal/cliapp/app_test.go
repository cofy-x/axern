package cliapp

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNewRegistersCoreCommands(t *testing.T) {
	app := New("test")
	commands := map[string]bool{}
	for _, command := range app.Commands() {
		commands[command.Name()] = true
	}
	for _, name := range []string{"task", "profile", "rollout", "export", "validate", "serve"} {
		if !commands[name] {
			t.Fatalf("command %q is not registered", name)
		}
	}
}

func TestHelpIncludesCompletionAndStableRolloutCommands(t *testing.T) {
	root := New("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"completion", "profile", "rollout", "validate", "export", "serve"} {
		if !strings.Contains(out.String(), name) {
			t.Fatalf("help missing %q:\n%s", name, out.String())
		}
	}
}

func TestExecuteClassifiesUnknownCommandAsUsageError(t *testing.T) {
	err := Execute(New("test"), []string{"obsolete-command"})
	var usage UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Execute() error = %v, want UsageError", err)
	}
}

func TestRemovedTopLevelRunIsRejected(t *testing.T) {
	err := Execute(New("test"), []string{"run", "--resume", "/tmp/run"})
	var usage UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Execute() error = %v, want UsageError", err)
	}
}
