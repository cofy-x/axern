package sandbox

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/urfave/cli"
)

type killRPCClient interface {
	KillSandbox(sandboxID, signal string) (*nodeoperatorv1.KillSandboxResponse, error)
	Close() error
}

var newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) {
	return client.New(ctx)
}

var KillCmd = cli.Command{
	Name:  "kill",
	Usage: "Send a signal to a running sandbox on the current node",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "signal, s",
			Value: "TERM",
			Usage: "signal name or number to send",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one sandbox id must be specified")
		}
		signal := strings.TrimSpace(context.String("signal"))
		if signal == "" {
			return fmt.Errorf("signal must not be empty")
		}

		opsClient, err := newKillRPCClient(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		if _, err := opsClient.KillSandbox(context.Args().First(), signal); err != nil {
			return fmt.Errorf("kill failed: %v", err)
		}
		return nil
	},
}
