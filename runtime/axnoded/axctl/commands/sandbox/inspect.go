package sandbox

import (
	"fmt"
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var InspectCmd = cli.Command{
	Name:  "inspect",
	Usage: "Inspect an Allocation on the current node",
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one allocation id must be specified")
		}
		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.GetAllocation(context.Args().First())
		if err != nil {
			return err
		}
		renderSandboxInspect(os.Stdout, resp.GetAllocation())
		return nil
	},
}
