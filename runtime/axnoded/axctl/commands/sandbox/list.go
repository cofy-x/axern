package sandbox

import (
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var ListCmd = cli.Command{
	Name:  "list",
	Usage: "List Allocations on the current node",
	Action: func(context *cli.Context) error {
		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.ListAllocations()
		if err != nil {
			return err
		}
		renderSandboxTable(os.Stdout, resp.GetAllocations())
		return nil
	},
}

var Command = cli.Command{
	Name:  "allocation",
	Usage: "Inspect and debug Allocations on the current node",
	Subcommands: []cli.Command{
		ListCmd,
		InspectCmd,
		DiagnosticsCmd,
		NetworkPolicyCmd,
		MemoryCmd,
		ExecCmd,
		WaitCmd,
		KillCmd,
		DeleteCmd,
	},
}
