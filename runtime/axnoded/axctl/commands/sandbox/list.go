package sandbox

import (
	"os"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var ListCmd = cli.Command{
	Name:  "list",
	Usage: "List sandboxes on the current node",
	Action: func(context *cli.Context) error {
		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		resp, err := opsClient.ListSandboxes()
		if err != nil {
			return err
		}
		renderSandboxTable(os.Stdout, resp.GetSandboxes())
		return nil
	},
}

var Command = cli.Command{
	Name:  "sandbox",
	Usage: "Inspect and operate sandboxes on the current node",
	Subcommands: []cli.Command{
		ListCmd,
		InspectCmd,
		DiagnosticsCmd,
		ExecCmd,
		WaitCmd,
		KillCmd,
		DeleteCmd,
	},
}
