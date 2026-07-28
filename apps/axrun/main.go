package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cofy-x/axern/apps/axrun/internal/cliapp"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command := cliapp.New(version)
	command.SetContext(ctx)
	if err := cliapp.Execute(command, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "axrun: %s\n", err)
		var usageErr cliapp.UsageError
		if errors.As(err, &usageErr) {
			os.Exit(2)
		}
		var exitErr cliapp.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
