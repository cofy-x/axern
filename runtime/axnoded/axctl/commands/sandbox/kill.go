package sandbox

import (
	"fmt"

	nodeoperatorv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/operator/v1"
	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/urfave/cli"
)

type killRPCClient interface {
	ForceTerminateAllocation(allocationID, reason string) (*nodeoperatorv1.ForceTerminateAllocationResponse, error)
	Close() error
}

var newKillRPCClient = func(ctx *cli.Context) (killRPCClient, error) {
	return client.New(ctx)
}

var KillCmd = cli.Command{
	Name:  "force-terminate",
	Usage: "Break glass: force-terminate an Allocation and report an operator diagnostic",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "reason",
			Usage: "required audited reason for the break-glass action",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one allocation id must be specified")
		}
		reason := context.String("reason")
		if reason == "" {
			return fmt.Errorf("--reason is required")
		}

		opsClient, err := newKillRPCClient(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		if _, err := opsClient.ForceTerminateAllocation(context.Args().First(), reason); err != nil {
			return fmt.Errorf("force terminate allocation: %v", err)
		}
		return nil
	},
}
