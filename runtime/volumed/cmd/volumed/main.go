package main

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/volumed/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "volumed: %v\n", err)
		os.Exit(1)
	}
}
