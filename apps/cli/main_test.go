package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCLIGroupUsage(t *testing.T) {
	if os.Getenv("AXERN_TEST_CLI_MAIN") == "1" {
		os.Args = append(os.Args[:1], os.Args[3:]...)
		main()
		os.Exit(0)
	}

	const nodeHelp = "Usage:\n  axern admin node [command]"
	for _, test := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{name: "root typo", args: []string{"unknown-root-command"}, code: 2,
			stderr: "axern: unknown command \"unknown-root-command\" for \"axern\"\n"},
		{name: "JSON nested typo", args: []string{"--output", "json", "admin", "node", "get", "node-example"}, code: 2,
			stderr: "axern: unknown command \"get\" for \"axern admin node\"\n"},
		{name: "nested typo with intervening flags", args: []string{"admin", "--context", "fixture", "node", "-o", "json", "get"}, code: 2,
			stderr: "axern: unknown command \"get\" for \"axern admin node\"\n"},
		{name: "literal after delimiter", args: []string{"admin", "node", "--", "--help"}, code: 2,
			stderr: "axern: unknown command \"--help\" for \"axern admin node\"\n"},
		{name: "bare group", args: []string{"--output", "json", "admin", "node"}, stdout: nodeHelp},
		{name: "bare group with invalid output value", args: []string{"admin", "node", "--output", "bogus"}, stdout: nodeHelp},
		{name: "empty after delimiter", args: []string{"admin", "node", "--"}, stdout: nodeHelp},
		{name: "explicit help", args: []string{"admin", "node", "--help"}, stdout: nodeHelp},
		{name: "explicit help with extra word", args: []string{"admin", "node", "get", "-h"}, stdout: nodeHelp},
		{name: "help command", args: []string{"help", "admin", "node"}, stdout: nodeHelp},
		{name: "generate completion script", args: []string{"completion", "bash"}, stdout: "__start_axern()"},
		{name: "complete command", args: []string{"__complete", "admin", "node", "lis"}, stdout: "list",
			stderr: "Completion ended with directive: ShellCompDirectiveNoFileComp\n"},
		{name: "complete without descriptions", args: []string{"__completeNoDesc", "admin", "node", "lis"}, stdout: "list\n:4\n",
			stderr: "Completion ended with directive: ShellCompDirectiveNoFileComp\n"},
		{name: "inherited flag error", args: []string{"admin", "node", "--bogus"}, code: 2,
			stderr: "axern: unknown flag: --bogus\n"},
		{name: "leaf argument validation", args: []string{"admin", "node", "list", "extra"}, code: 2,
			stderr: "axern: unknown command \"extra\" for \"axern admin node list\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], append([]string{"-test.run=^TestCLIGroupUsage$", "--"}, test.args...)...)
			cmd.Env = append(os.Environ(), "AXERN_TEST_CLI_MAIN=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			code := 0
			if err := cmd.Run(); err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != test.code || stderr.String() != test.stderr ||
				test.stdout == "" && stdout.Len() != 0 || !strings.Contains(stdout.String(), test.stdout) {
				t.Fatalf("exit=%d, stdout=%q, stderr=%q, want exit=%d, stdout containing %q, stderr=%q",
					code, stdout.String(), stderr.String(), test.code, test.stdout, test.stderr)
			}
		})
	}
}
