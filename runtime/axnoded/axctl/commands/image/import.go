package image

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli"
)

type importRequest struct {
	ArchivePath string `json:"archive_path"`
	ImageRef    string `json:"image_ref"`
}

type importResponse struct {
	ImageRef       string `json:"image_ref"`
	ArchivePath    string `json:"archive_path"`
	ArchiveDigest  string `json:"archive_digest,omitempty"`
	SizeBytes      int64  `json:"size_bytes"`
	ImportedAtUnix int64  `json:"imported_at_unix"`
}

var ImportCmd = cli.Command{
	Name:  "import",
	Usage: "Import a Docker archive into the local imagemgr OCI cache",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "archive",
			Usage: "Path to a Docker archive produced by docker save",
		},
		cli.StringFlag{
			Name:  "ref",
			Usage: "Image ref to register the archive under",
		},
		imagemgrSocketFlag(),
		cli.BoolFlag{
			Name:  "json",
			Usage: "Print JSON output",
		},
	},
	Action: func(context *cli.Context) error {
		archivePath := strings.TrimSpace(context.String("archive"))
		imageRef := strings.TrimSpace(context.String("ref"))
		if archivePath == "" {
			return cli.NewExitError("--archive is required", 2)
		}
		if imageRef == "" {
			return cli.NewExitError("--ref is required", 2)
		}

		resp, err := postImport(context.String("imagemgr-socket"), importRequest{
			ArchivePath: archivePath,
			ImageRef:    imageRef,
		}, context.GlobalDuration("timeout"))
		if err != nil {
			return err
		}
		if context.Bool("json") {
			return json.NewEncoder(os.Stdout).Encode(resp)
		}
		fmt.Printf("Imported image %s (%d bytes, digest %s)\n", resp.ImageRef, resp.SizeBytes, emptyDash(resp.ArchiveDigest))
		return nil
	},
}

var Command = cli.Command{
	Name:        "image",
	Usage:       "Manage local node image cache",
	Subcommands: []cli.Command{ImportCmd, ListCmd, InspectCmd, MountsCmd},
}
