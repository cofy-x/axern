package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/examples/internal/exampleutil"
)

func main() {
	config := exampleutil.Flags()
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := exampleutil.NewClient(ctx, config)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	sandbox, err := exampleutil.StartSandbox(ctx, client, config)
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Close(context.Background())

	if err := exampleutil.PrintMetadata(sandbox); err != nil {
		log.Fatal(err)
	}
	result, err := sandbox.Exec(ctx, axern.Shell("python -c \"print('hello from axern go sdk')\""), axern.ExecOptions{Check: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(result.StdoutString())

	_, err = sandbox.Exec(ctx, axern.Args("python", "-c", "import sys; sys.exit(7)"), axern.ExecOptions{Check: true})
	var execErr *axern.ExecError
	if errors.As(err, &execErr) {
		fmt.Printf("checked command exit=%d\n", execErr.ExitCode())
	} else if err != nil {
		log.Fatal(err)
	}
}
