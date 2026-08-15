package image

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli"
)

type importRequest struct {
	ImageRef string
	Archive  io.Reader
}

type importResponse struct {
	SourceRef        string `json:"source_ref"`
	CanonicalRef     string `json:"canonical_ref"`
	ImmutableRef     string `json:"immutable_ref"`
	GenerationDigest string `json:"generation_digest"`
	ArchiveDigest    string `json:"archive_digest"`
	Platform         string `json:"platform"`
	SizeBytes        int64  `json:"size_bytes"`
	Reused           bool   `json:"reused"`
}

var ImportCmd = cli.Command{
	Name:  "import",
	Usage: "Import a Docker archive into the local imagemgr OCI cache",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "file",
			Value: "-",
			Usage: "Docker archive path, or - to stream from stdin",
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
		archivePath := strings.TrimSpace(context.String("file"))
		imageRef := strings.TrimSpace(context.String("ref"))
		if archivePath == "" {
			return cli.NewExitError("--file is required", 2)
		}
		if imageRef == "" {
			return cli.NewExitError("--ref is required", 2)
		}

		archive := io.Reader(os.Stdin)
		var file *os.File
		if archivePath != "-" {
			var err error
			file, err = os.Open(archivePath)
			if err != nil {
				return fmt.Errorf("open archive %s: %w", archivePath, err)
			}
			defer file.Close()
			archive = file
		}
		resp, err := postImport(context.String("imagemgr-socket"), importRequest{ImageRef: imageRef, Archive: archive}, context.GlobalDuration("timeout"))
		if err != nil {
			return err
		}
		if context.Bool("json") {
			return json.NewEncoder(os.Stdout).Encode(resp)
		}
		fmt.Printf("Imported %s as %s\n", resp.SourceRef, resp.ImmutableRef)
		fmt.Printf("  generation: %s\n  archive: %s\n  platform: %s\n  size: %d bytes\n  reused: %t\n", resp.GenerationDigest, resp.ArchiveDigest, resp.Platform, resp.SizeBytes, resp.Reused)
		return nil
	},
}

var Command = cli.Command{
	Name:        "image",
	Usage:       "Manage local node image cache",
	Subcommands: []cli.Command{ImportCmd, ListCmd, InspectCmd, MountsCmd, DropPageCacheCmd},
}
