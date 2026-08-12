package image

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/urfave/cli"
)

const maxPageCacheDropBytes int64 = 1 << 30

const (
	maxPageCacheImageRefBytes = 2048
	maxPageCachePathBytes     = 4096
)

type dropPageCacheRequest struct {
	ImageRef      string
	ContainerPath string
	OffsetBytes   int64
	LengthBytes   int64
}

type dropPageCacheResult struct {
	ImageRef      string `json:"image_ref"`
	MountType     string `json:"mount_type"`
	MountPath     string `json:"mount_path"`
	ContainerPath string `json:"container_path"`
	OffsetBytes   int64  `json:"offset_bytes"`
	LengthBytes   int64  `json:"length_bytes"`
	PageSizeBytes int64  `json:"page_size_bytes"`
	ResidentPages int    `json:"resident_pages_after"`
}

var DropPageCacheCmd = cli.Command{
	Name:  "drop-page-cache",
	Usage: "Evict and verify a regular-file range from one mounted image",
	Flags: []cli.Flag{
		cli.StringFlag{Name: "ref", Usage: "Exact mounted image, Nydus, or cache reference"},
		cli.StringFlag{Name: "path", Usage: "Absolute regular-file path inside the image"},
		cli.Int64Flag{Name: "offset", Usage: "Byte offset aligned to the host page size"},
		cli.Int64Flag{Name: "length", Usage: "Positive byte length, at most 1 GiB"},
		imagemgrSocketFlag(),
		cli.BoolFlag{Name: "json", Usage: "Print JSON output"},
	},
	Action: func(context *cli.Context) error {
		request := dropPageCacheRequest{
			ImageRef:      strings.TrimSpace(context.String("ref")),
			ContainerPath: strings.TrimSpace(context.String("path")),
			OffsetBytes:   context.Int64("offset"),
			LengthBytes:   context.Int64("length"),
		}
		if err := request.validate(); err != nil {
			return cli.NewExitError(err.Error(), 2)
		}
		inventory, err := fetchInventory(
			context.String("imagemgr-socket"),
			context.GlobalDuration("timeout"),
		)
		if err != nil {
			return err
		}
		mount, err := uniqueMountedImage(inventory, request.ImageRef)
		if err != nil {
			return err
		}
		if err := dropMountedFilePageCache(
			mount.MountPath,
			request.ContainerPath,
			request.OffsetBytes,
			request.LengthBytes,
		); err != nil {
			return fmt.Errorf(
				"drop page cache for image %s path %s: %w",
				request.ImageRef,
				request.ContainerPath,
				err,
			)
		}
		afterInventory, err := fetchInventory(
			context.String("imagemgr-socket"),
			context.GlobalDuration("timeout"),
		)
		if err != nil {
			return fmt.Errorf("revalidate image mount after page-cache eviction: %w", err)
		}
		afterMount, err := uniqueMountedImage(afterInventory, request.ImageRef)
		if err != nil {
			return fmt.Errorf("revalidate image mount after page-cache eviction: %w", err)
		}
		if afterMount.MountPath != mount.MountPath || afterMount.MountType != mount.MountType {
			return fmt.Errorf("mounted image identity changed during page-cache eviction")
		}
		result := dropPageCacheResult{
			ImageRef:      request.ImageRef,
			MountType:     mount.MountType,
			MountPath:     mount.MountPath,
			ContainerPath: request.ContainerPath,
			OffsetBytes:   request.OffsetBytes,
			LengthBytes:   request.LengthBytes,
			PageSizeBytes: int64(os.Getpagesize()),
			ResidentPages: 0,
		}
		if context.Bool("json") {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		fmt.Printf(
			"Dropped page cache: image=%s path=%s offset=%d length=%d\n",
			result.ImageRef,
			result.ContainerPath,
			result.OffsetBytes,
			result.LengthBytes,
		)
		return nil
	},
}

func (request dropPageCacheRequest) validate() error {
	if request.ImageRef == "" {
		return fmt.Errorf("--ref is required")
	}
	if len(request.ImageRef) > maxPageCacheImageRefBytes {
		return fmt.Errorf("--ref exceeds %d bytes", maxPageCacheImageRefBytes)
	}
	if request.ContainerPath == "" {
		return fmt.Errorf("--path is required")
	}
	if len(request.ContainerPath) > maxPageCachePathBytes {
		return fmt.Errorf("--path exceeds %d bytes", maxPageCachePathBytes)
	}
	if !strings.HasPrefix(request.ContainerPath, "/") || path.Clean(request.ContainerPath) != request.ContainerPath || request.ContainerPath == "/" {
		return fmt.Errorf("--path must be a clean absolute non-root path")
	}
	if request.OffsetBytes < 0 {
		return fmt.Errorf("--offset must be non-negative")
	}
	if request.LengthBytes <= 0 || request.LengthBytes > maxPageCacheDropBytes {
		return fmt.Errorf("--length must be between 1 and %d bytes", maxPageCacheDropBytes)
	}
	pageSize := int64(os.Getpagesize())
	if request.OffsetBytes%pageSize != 0 || request.LengthBytes%pageSize != 0 {
		return fmt.Errorf("--offset and --length must be aligned to the %d-byte host page size", pageSize)
	}
	return nil
}

func uniqueMountedImage(inventory *inventoryResponse, imageRef string) (mountedImageDetail, error) {
	if inventory == nil {
		return mountedImageDetail{}, fmt.Errorf("imagemgr inventory is unavailable")
	}
	matches := make([]mountedImageDetail, 0, 1)
	for _, mount := range inventory.MountedImages {
		if strings.TrimSpace(mount.MountPath) == "" || strings.TrimSpace(mount.MountType) == "" {
			continue
		}
		if mount.ImageURL == imageRef || mount.NydusImageURL == imageRef || mount.CacheKey == imageRef {
			matches = append(matches, mount)
		}
	}
	if len(matches) != 1 {
		return mountedImageDetail{}, fmt.Errorf(
			"mounted image reference %q matched %d records, want exactly one",
			imageRef,
			len(matches),
		)
	}
	return matches[0], nil
}
