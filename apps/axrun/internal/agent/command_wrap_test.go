package agent

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestWrapCommandWithShellPreludePreservesShellCommand(t *testing.T) {
	command := WrapCommandWithShellPrelude("set -eu\nprintf setup", sandbox.ShellCommand("printf run"))
	if got := command.Shell(); !strings.Contains(got, "set -eu\nprintf setup") || !strings.Contains(got, "\nprintf run") {
		t.Fatalf("shell = %q", got)
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestWrapCommandWithShellPreludeDoesNotLeakShellOptions(t *testing.T) {
	command := WrapCommandWithShellPrelude("set -u\n: \"$AXERN_PRELUDE_SET\"", sandbox.ShellCommand("printf '%s' \"$OPTIONAL_AGENT_VALUE\""))
	shell := command.Shell()
	if !strings.Contains(shell, "set -u") || !strings.Contains(shell, "\nprintf") {
		t.Fatalf("shell = %q", shell)
	}
}

func TestWrapCommandWithShellPreludePreservesExportedEnv(t *testing.T) {
	command := WrapCommandWithShellPrelude("set -eu\nexport AXERN_PRELUDE_VALUE=ok", sandbox.ShellCommand(`printf '%s' "$AXERN_PRELUDE_VALUE"`))
	output, err := exec.Command("/bin/sh", "-c", command.Shell()).CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped command failed: %v\n%s", err, output)
	}
	if string(output) != "ok" {
		t.Fatalf("output = %q", output)
	}
}

func TestWrapCommandWithShellPreludeRestoresShellOptions(t *testing.T) {
	command := WrapCommandWithShellPrelude("set -u", sandbox.ShellCommand(`printf '%s' "$OPTIONAL_AGENT_VALUE"`))
	output, err := exec.Command("/bin/sh", "-c", command.Shell()).CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped command failed: %v\n%s", err, output)
	}
	if string(output) != "" {
		t.Fatalf("output = %q", output)
	}
}

func TestWrapCommandWithShellPreludeQuotesArgvCommand(t *testing.T) {
	command := WrapCommandWithShellPrelude("printf setup", sandbox.ArgvCommand([]string{"bash", "-lc", "printf '%s'\n"}))
	shell := command.Shell()
	if !strings.Contains(shell, "exec 'bash' '-lc' 'printf '\\''%s'\\''") {
		t.Fatalf("shell = %q", shell)
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}
