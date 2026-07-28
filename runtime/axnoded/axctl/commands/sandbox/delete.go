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
	Name:  "delete",
	Usage: "Force-delete a sandbox from the current node as a local operator action",
	Flags: []cli.Flag{
		cli.DurationFlag{
			Name:  "timeout",
			Value: config.StopTimeout,
			Usage: "grace period before axnoded falls back to force delete; set 0 to force delete immediately",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() == 0 {
			return fmt.Errorf("no sandbox id specified")
		}

		opsClient, err := client.New(context)
		if err != nil {
			return err
		}
		defer opsClient.Close()

		for _, sandboxID := range context.Args() {
			if _, err := opsClient.DeleteSandbox(sandboxID, deleteTimeoutSeconds(context.Duration("timeout"))); err != nil {
				return fmt.Errorf("delete sandbox %s: %v", sandboxID, err)
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
