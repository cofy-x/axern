package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/cofy-x/axern/apps/cli/internal/cliapp"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	axernsdk "github.com/cofy-x/axern/sdk/go"
)

func main() {
	if err := cliapp.Execute(cliapp.New(axernsdk.Version()), os.Args[1:]); err != nil {
		if err.Error() != "" {
			fmt.Fprintf(os.Stderr, "axern: %s\n", err)
		}
		var usageError command.UsageError
		if errors.As(err, &usageError) {
			os.Exit(2)
		}
		var exitError command.ExitError
		if errors.As(err, &exitError) {
			os.Exit(exitError.Code)
		}
		os.Exit(1)
	}
}
