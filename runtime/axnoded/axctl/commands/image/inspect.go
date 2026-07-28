package image

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
)

var InspectCmd = cli.Command{
	Name:  "inspect",
	Usage: "Inspect one image ref in the local imagemgr cache",
	Flags: []cli.Flag{
		imagemgrSocketFlag(),
		cli.BoolFlag{
			Name:  "json",
			Usage: "Print JSON output",
		},
	},
	Action: func(context *cli.Context) error {
		if context.NArg() != 1 {
			return fmt.Errorf("exactly one image ref must be specified")
		}
		imageRef := context.Args().First()
		inventory, err := fetchInventory(context.String("imagemgr-socket"), context.GlobalDuration("timeout"))
		if err != nil {
			return err
		}
		row, ok := findImageRow(buildImageRows(inventory), imageRef)
		if !ok {
			return fmt.Errorf("image ref not found in local imagemgr inventory: %s", imageRef)
		}
		if context.Bool("json") {
			return renderJSON(os.Stdout, row)
		}
		renderImageInspect(os.Stdout, row)
		return nil
	},
}
