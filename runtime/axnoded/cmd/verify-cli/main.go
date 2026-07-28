package main

import (
	"fmt"
	"os"
)

func main() {
	cfg := parseFlags()
	if err := runVerifyCLI(cfg); err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
