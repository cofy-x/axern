package image

import (
	"os"

	"github.com/urfave/cli"
)

var ListCmd = cli.Command{
	Name:  "list",
	Usage: "List imported and mounted images in the local imagemgr cache",
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
		rows := buildImageRows(inventory)
		if context.Bool("json") {
			return renderJSON(os.Stdout, imageListJSON{Images: rows})
		}
		renderImageListTable(os.Stdout, rows)
		return nil
	},
}
