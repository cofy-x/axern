// Command controld-maintenance operates on a stopped native installation.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) >= 2 {
		switch args[0] {
		case "upgrade":
			if args[1] == "check" {
				return runNativeUpgradeCheck(ctx, args[2:])
			}
		case "restore":
			if args[1] == "plan" || args[1] == "commit" {
				return runNativeRestore(ctx, args[1], args[2:])
			}
		}
	}
	return errors.New("usage: controld-maintenance {upgrade check | restore plan | restore commit} [flags]")
}
