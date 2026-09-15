package sandbox

import (
	"fmt"
	"math"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/axctl/client"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/urfave/cli"
)

var DeleteCmd = cli.Command{
	Name:  "force-cleanup",
	Usage: "Break glass: force terminal cleanup through the Allocation recovery path",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "reason",
			Usage: "required audited reason for the break-glass action",
		},
		cli.DurationFlag{
			Name:  "timeout",
			Value: config.StopTimeout,
			Usage: "grace period before axnoded falls back to force delete; set 0 to force delete immediately",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() == 0 {
			return fmt.Errorf("no allocation id specified")
		}
		reason := context.String("reason")
		if reason == "" {
			return fmt.Errorf("--reason is required")
		}

		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		for _, allocationID := range context.Args() {
			if _, err := opsClient.ForceCleanupAllocation(allocationID, reason, deleteTimeoutSeconds(context.Duration("timeout"))); err != nil {
				return fmt.Errorf("force cleanup allocation %s: %v", allocationID, err)
			}
		}
		return nil
	},
}

func deleteTimeoutSeconds(timeout time.Duration) int64 {
	if timeout <= 0 {
		return 0
	}
	return int64(math.Ceil(timeout.Seconds()))
}
