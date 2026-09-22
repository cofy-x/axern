package main

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/internal/app"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "base-spec" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: axnoded base-spec OUTPUT")
			os.Exit(2)
		}
		if err := oci.WriteBaseSpec(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "axnoded base-spec: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "axnoded: %v\n", err)
		os.Exit(1)
	}
}
