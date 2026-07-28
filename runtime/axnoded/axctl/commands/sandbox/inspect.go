package sandbox

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var InspectCmd = cli.Command{
	Name:  "inspect",
	Usage: "Inspect a sandbox on the current node",
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one sandbox id must be specified")
		}
		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.GetSandbox(context.Args().First())
		if err != nil {
			return err
		}
		renderSandboxInspect(os.Stdout, resp.GetSandbox())
		return nil
	},
}
