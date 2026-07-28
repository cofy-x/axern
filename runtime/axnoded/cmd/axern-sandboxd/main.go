package main

import (
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd"
)

func main() {
	os.Exit(sandboxd.RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
