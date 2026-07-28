package main

import (
	"fmt"
	"os"
)

func main() {
	cfg := parseFlags()
	if err := runVerifySmoke(cfg); err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
