package node

import (
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

var CheckCmd = cli.Command{
	Name:  "check",
	Usage: "Print the local axnoded health status",
	Action: func(context *cli.Context) error {
		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		fmt.Printf("Healthz status: %+v \n", opsClient.Healthz())
		return nil
	},
}

var Command = cli.Command{
	Name:        "node",
	Usage:       "Inspect the current axnoded node",
	Subcommands: []cli.Command{CheckCmd, ResourcesCmd},
}
