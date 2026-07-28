package image

import (
	"os"

	"github.com/urfave/cli"
)

var MountsCmd = cli.Command{
	Name:  "mounts",
	Usage: "List mounted images in the local imagemgr cache",
	Flags: []cli.Flag{
		imagemgrSocketFlag(),
		cli.BoolFlag{
			Name:  "json",
			Usage: "Print JSON output",
		},
	},
	Action: func(context *cli.Context) error {
		inventory, err := fetchInventory(context.String("imagemgr-socket"), context.GlobalDuration("timeout"))
		if err != nil {
			return err
		}
		mounts := sortedMounts(inventory)
		if context.Bool("json") {
			return renderJSON(os.Stdout, mountsJSON{Mounts: mounts})
		}
		renderMountsTable(os.Stdout, mounts)
		return nil
	},
}
