package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/app"
	"github.com/urfave/cli"
)

var pluginCmds = []cli.Command{}

func main() {
	app := app.New()
	app.Commands = append(app.Commands, pluginCmds...)
	if err := app.Run(normalizeArgs(os.Args)); err != nil {
		if exitErr, ok := err.(cli.ExitCoder); ok {
			if message := exitErr.Error(); message != "" {
				fmt.Fprintf(os.Stderr, "axctl: %s\n", message)
			}
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "axctl: %s\n", err)
		os.Exit(1)
	}
}

func normalizeArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}

	normalized := make([]string, 0, len(args)+2)
	normalized = append(normalized, args[0])

	seenSandbox := false
	seenSandboxExec := false
	for _, arg := range args[1:] {
		if !seenSandboxExec {
			normalized = append(normalized, arg)
			if arg == "sandbox" {
				seenSandbox = true
				continue
			}
			if seenSandbox && arg == "exec" {
				seenSandboxExec = true
			}
			continue
		}
		if arg == "--" {
			normalized = append(normalized, arg)
			seenSandboxExec = false
			continue
		}
		if expanded, ok := expandExecShortFlags(arg); ok {
			normalized = append(normalized, expanded...)
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}

func expandExecShortFlags(arg string) ([]string, bool) {
	if len(arg) < 3 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return nil, false
	}

	flags := make([]string, 0, len(arg)-1)
	for _, ch := range arg[1:] {
		switch ch {
		case 'i', 't':
			flags = append(flags, "-"+string(ch))
		default:
			return nil, false
		}
	}
	return flags, true
}
