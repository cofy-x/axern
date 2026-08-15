package image

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type mountedImageDetail struct {
	ImageURL      string `json:"image_url"`
	CacheKey      string `json:"cache_key,omitempty"`
	MountType     string `json:"mount_type"`
	NydusImageURL string `json:"nydus_image_url,omitempty"`
	MountPath     string `json:"mount_path"`
}

type importedImageDetail struct {
	ImageRef         string `json:"image_ref"`
	GenerationDigest string `json:"generation_digest"`
	ArchiveDigest    string `json:"archive_digest"`
	Platform         string `json:"platform"`
	SizeBytes        int64  `json:"size_bytes"`
	ImportedAtUnix   int64  `json:"imported_at_unix"`
}

type inventoryResponse struct {
	MountedImages  []mountedImageDetail  `json:"mounted_images"`
	ImportedImages []importedImageDetail `json:"imported_images,omitempty"`
}

type imageRow struct {
	ImageRef         string `json:"image_ref"`
	Imported         bool   `json:"imported"`
	Mounted          bool   `json:"mounted"`
	MountType        string `json:"mount_type"`
	MountPath        string `json:"mount_path"`
	NydusImageURL    string `json:"nydus_image_url"`
	GenerationDigest string `json:"generation_digest"`
	ArchiveDigest    string `json:"archive_digest"`
	Platform         string `json:"platform"`
	CacheKey         string `json:"cache_key"`
	SizeBytes        int64  `json:"size_bytes"`
	ImportedAtUnix   int64  `json:"imported_at_unix"`
}

type imageListJSON struct {
	Images []imageRow `json:"images"`
}

type mountsJSON struct {
	Mounts []mountedImageDetail `json:"mounts"`
}

func buildImageRows(inventory *inventoryResponse) []imageRow {
	if inventory == nil {
		return nil
	}
	byRef := make(map[string]*imageRow)
	for _, imported := range inventory.ImportedImages {
		if imported.ImageRef == "" {
			continue
		}
		row := imageRowForRef(byRef, imported.ImageRef)
		row.Imported = true
		row.GenerationDigest = imported.GenerationDigest
		row.ArchiveDigest = imported.ArchiveDigest
		row.Platform = imported.Platform
		row.SizeBytes = imported.SizeBytes
		row.ImportedAtUnix = imported.ImportedAtUnix
	}
	for _, mounted := range inventory.MountedImages {
		if mounted.ImageURL == "" {
			continue
		}
		row := imageRowForRef(byRef, mounted.ImageURL)
		row.Mounted = true
		row.MountType = mounted.MountType
		row.MountPath = mounted.MountPath
		row.NydusImageURL = mounted.NydusImageURL
		row.CacheKey = mounted.CacheKey
	}

	rows := make([]imageRow, 0, len(byRef))
	for _, row := range byRef {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ImageRef < rows[j].ImageRef
	})
	return rows
}

func imageRowForRef(byRef map[string]*imageRow, ref string) *imageRow {
	row := byRef[ref]
	if row == nil {
		row = &imageRow{ImageRef: ref}
		byRef[ref] = row
	}
	return row
}

func findImageRow(rows []imageRow, imageRef string) (imageRow, bool) {
	imageRef = strings.TrimSpace(imageRef)
	for _, row := range rows {
		if row.ImageRef == imageRef || row.GenerationDigest == imageRef || row.ArchiveDigest == imageRef || row.CacheKey == imageRef {
			return row, true
		}
	}
	return imageRow{}, false
}

func sortedMounts(inventory *inventoryResponse) []mountedImageDetail {
	if inventory == nil {
		return nil
	}
	mounts := make([]mountedImageDetail, 0, len(inventory.MountedImages))
	for _, mount := range inventory.MountedImages {
		if mount.ImageURL == "" {
			continue
		}
		mounts = append(mounts, mount)
	}
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].ImageURL < mounts[j].ImageURL
	})
	return mounts
}

func renderImageListTable(w io.Writer, rows []imageRow) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\tIMPORTED\tMOUNTED\tMOUNT TYPE\tGENERATION\tPLATFORM\tSIZE\tIMPORTED AT")
	for _, row := range rows {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ImageRef,
			yesNo(row.Imported),
			yesNo(row.Mounted),
			emptyDash(row.MountType),
			emptyDash(shortIdentity(row.GenerationDigest)),
			emptyDash(row.Platform),
			formatSize(row.SizeBytes),
			formatUnix(row.ImportedAtUnix),
		)
	}
	_ = tw.Flush()
}

func renderImageInspect(w io.Writer, row imageRow) {
	fmt.Fprintf(w, "Image Ref: %s\n", row.ImageRef)
	fmt.Fprintf(w, "Imported: %s\n", yesNo(row.Imported))
	fmt.Fprintf(w, "Generation Digest: %s\n", emptyDash(row.GenerationDigest))
	fmt.Fprintf(w, "Archive Digest: %s\n", emptyDash(row.ArchiveDigest))
	fmt.Fprintf(w, "Platform: %s\n", emptyDash(row.Platform))
	fmt.Fprintf(w, "Size: %s\n", formatSize(row.SizeBytes))
	fmt.Fprintf(w, "Imported At: %s\n", formatUnix(row.ImportedAtUnix))
	fmt.Fprintf(w, "Mounted: %s\n", yesNo(row.Mounted))
	fmt.Fprintf(w, "Mount Type: %s\n", emptyDash(row.MountType))
	fmt.Fprintf(w, "Mount Path: %s\n", emptyDash(row.MountPath))
	fmt.Fprintf(w, "Cache Key: %s\n", emptyDash(row.CacheKey))
	fmt.Fprintf(w, "Nydus Image Ref: %s\n", emptyDash(row.NydusImageURL))
}

func renderMountsTable(w io.Writer, mounts []mountedImageDetail) {
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\tMOUNT TYPE\tCACHE KEY\tMOUNT PATH\tNYDUS REF")
	for _, mount := range mounts {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\n",
			mount.ImageURL,
			emptyDash(mount.MountType),
			emptyDash(shortIdentity(mount.CacheKey)),
			emptyDash(mount.MountPath),
			emptyDash(mount.NydusImageURL),
		)
	}
	_ = tw.Flush()
}

func renderJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func shortIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 32 {
		return value
	}
	return value[:29] + "..."
}

func formatSize(size int64) string {
	if size <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", size)
}

func formatUnix(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}
