package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
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

	process, err := sandbox.Process(ctx, axern.Args("python", "-u", "-c", "import sys; print(sys.stdin.read().upper())"), axern.ProcessOptions{Timeout: 30 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	defer process.Close()

	if err := process.WriteString("streamed from go\n"); err != nil {
		log.Fatal(err)
	}
	if err := process.CloseStdin(); err != nil {
		log.Fatal(err)
	}
	output, err := process.Output()
	if err != nil {
		log.Fatal(err)
	}
	if output.ExitCode != 0 {
		log.Fatalf("process exited with %d: %s stderr=%q", output.ExitCode, output.Message, output.Stderr)
	}
	fmt.Println(strings.TrimSpace(string(output.Stdout)))
}
