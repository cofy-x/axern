package main

import (
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == proc.InternalExecCommand {
		os.Exit(proc.RunInternalExec(os.Args[2:], os.Stderr))
	}
	os.Exit(sandboxd.RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
