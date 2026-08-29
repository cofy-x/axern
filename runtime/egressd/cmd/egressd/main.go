package main

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/egressd/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "egressd: %v\n", err)
		os.Exit(1)
	}
}
