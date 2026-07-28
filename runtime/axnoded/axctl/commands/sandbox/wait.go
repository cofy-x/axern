package sandbox

import (
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var WaitCmd = cli.Command{
	Name:  "wait",
	Usage: "Wait for a sandbox to exit and print its local exit status",
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one sandbox id must be specified")
		}
		sandboxID := context.Args().First()

		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.WaitSandbox(sandboxID, waitCommandTimeout(context))
		if err != nil {
			return fmt.Errorf("wait failed: %v", err)
		}

		fmt.Printf("Sandbox: %s\n", sandboxID)
		fmt.Printf("State: %s\n", localStateString(resp.GetState()))
		fmt.Printf("Exit Code: %s\n", localExitCodeString(resp.GetState(), resp.GetExitCode(), resp.GetExitCodeKnown()))
		if resp.GetMessage() != "" {
			fmt.Printf("Message: %s\n", resp.GetMessage())
		}
		return nil
	},
}

func waitCommandTimeout(ctx *cli.Context) time.Duration {
	if !ctx.GlobalIsSet("timeout") {
		return 0
	}
	return ctx.GlobalDuration("timeout")
}
