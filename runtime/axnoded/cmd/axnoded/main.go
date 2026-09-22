package main

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "axnoded: %v\n", err)
		os.Exit(1)
	}
}
