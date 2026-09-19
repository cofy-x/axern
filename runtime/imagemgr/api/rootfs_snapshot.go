package api

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const maxRootfsSnapshotLayerBytes = int64(32 << 30)

var errRootfsSnapshotLayerTooLarge = errors.New("rootfs snapshot layer exceeds its maximum size")

type RootfsSnapshotRequestError struct{ Err error }

func (e *RootfsSnapshotRequestError) Error() string { return e.Err.Error() }
func (e *RootfsSnapshotRequestError) Unwrap() error { return e.Err }

func invalidRootfsSnapshotRequest(format string, args ...any) error {
	return &RootfsSnapshotRequestError{Err: fmt.Errorf(format, args...)}
}

type RootfsSnapshotRequest struct {
	AllocationID           string `json:"allocation_id"`
	BaseImageRef           string `json:"base_image_ref"`
	BaseRegistryCredential string `json:"base_registry_credential_json,omitempty"`
	MaxBytes               string `json:"max_bytes"`
}

type RootfsSnapshotResponse struct {
	ImageRef        string `json:"image_ref"`
	Digest          string `json:"digest"`
	MediaType       string `json:"media_type"`
	SizeBytes       int64  `json:"size_bytes"`
	PlatformOS      string `json:"platform_os"`
	PlatformArch    string `json:"platform_arch"`
	PlatformVariant string `json:"platform_variant,omitempty"`
}

// SnapshotRootfs appends one stopped runsc upper layer to the immutable base
// image and publishes the resulting image to the platform-owned repository.
// The caller supplies bytes, never a host path, across the imagemgr boundary.
func (w *HttpWorker) SnapshotRootfs(ctx context.Context, req RootfsSnapshotRequest, upper io.Reader) (*RootfsSnapshotResponse, error) {
	allocationID := strings.TrimSpace(req.AllocationID)
	baseRef := strings.TrimSpace(req.BaseImageRef)
	repository := strings.TrimSpace(w.snapshotRepository)
	if allocationID == "" || baseRef == "" || upper == nil {
		return nil, invalidRootfsSnapshotRequest("allocation id, base image ref and upper layer are required")
	}
	if repository == "" || w.registry == nil || w.ociMgr == nil {
		return nil, fmt.Errorf("rootfs snapshot publication is not configured")
	}
	repo, err := name.NewRepository(repository, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("invalid snapshot repository %q: %w", repository, err)
	}
	tagHash := sha256.Sum256([]byte(allocationID))
	target := repo.Tag("allocation-" + hex.EncodeToString(tagHash[:16]))

	stagingDir := strings.TrimSpace(w.snapshotStagingDir)
	if stagingDir == "" {
		return nil, fmt.Errorf("rootfs snapshot staging is not configured")
	}
	tmp, err := os.CreateTemp(stagingDir, "upper-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create snapshot upper layer: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	maxBytes := maxRootfsSnapshotLayerBytes
	if raw := strings.TrimSpace(req.MaxBytes); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			return nil, invalidRootfsSnapshotRequest("snapshot maximum bytes must be positive")
		}
		if parsed < maxBytes {
			maxBytes = parsed
		}
	}
	limited := &io.LimitedReader{R: upper, N: maxBytes + 1}
	if err := writeOCIRootfsSnapshotLayer(limited, tmp, maxBytes); err != nil {
		tmp.Close()
		return nil, &RootfsSnapshotRequestError{Err: err}
	}
	_, drainErr := io.Copy(io.Discard, limited)
	if drainErr != nil {
		tmp.Close()
		return nil, fmt.Errorf("receive snapshot upper layer: %w", drainErr)
	}
	if limited.N <= 0 {
		tmp.Close()
		return nil, invalidRootfsSnapshotRequest("snapshot upper layer exceeds %d bytes", maxBytes)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("sync snapshot upper layer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close snapshot upper layer: %w", err)
	}
	base, err := w.ociMgr.ResolveImage(ctx, baseRef, req.BaseRegistryCredential)
	if err != nil {
		return nil, fmt.Errorf("load snapshot base image: %w", err)
	}
	layer, err := tarball.LayerFromFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("load snapshot upper layer: %w", err)
	}
	image, err := mutate.AppendLayers(base, layer)
	if err != nil {
		return nil, fmt.Errorf("append snapshot upper layer: %w", err)
	}
	if err := w.registry.WriteImageDirect(ctx, target.Name(), image); err != nil {
		return nil, err
	}
	digest, err := image.Digest()
	if err != nil {
		return nil, fmt.Errorf("read snapshot image digest: %w", err)
	}
	manifest, err := image.RawManifest()
	if err != nil {
		return nil, fmt.Errorf("read snapshot image manifest: %w", err)
	}
	mediaType, err := image.MediaType()
	if err != nil {
		return nil, fmt.Errorf("read snapshot image media type: %w", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read snapshot image platform: %w", err)
	}
	immutable := target.Context().Digest(digest.String()).Name()
	return &RootfsSnapshotResponse{
		ImageRef: immutable, Digest: digest.String(), MediaType: string(mediaType), SizeBytes: int64(len(manifest)),
		PlatformOS: config.OS, PlatformArch: config.Architecture, PlatformVariant: config.Variant,
	}, nil
}

type boundedSnapshotWriter struct {
	destination io.Writer
	remaining   int64
}

func (w *boundedSnapshotWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, errRootfsSnapshotLayerTooLarge
	}
	n, err := w.destination.Write(data)
	w.remaining -= int64(n)
	return n, err
}

// writeOCIRootfsSnapshotLayer translates gVisor's tmpfs overlay encoding into
// an OCI image layer. gVisor serializes deletions as 0:0 character devices and
// opaque directories as overlay xattrs; OCI represents both with .wh entries.
func writeOCIRootfsSnapshotLayer(source io.Reader, destination io.Writer, maxBytes int64) error {
	if source == nil || destination == nil || maxBytes <= 0 {
		return fmt.Errorf("snapshot upper layer source, destination and positive limit are required")
	}
	reader := tar.NewReader(source)
	writer := tar.NewWriter(&boundedSnapshotWriter{destination: destination, remaining: maxBytes})
	materialized := make(map[string]struct{})
	for {
		header, err := reader.Next()
		if err == io.EOF {
			if err := writer.Close(); err != nil {
				return fmt.Errorf("close OCI snapshot layer: %w", err)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read snapshot upper layer: %w", err)
		}
		clean, err := safeRootfsSnapshotPath(header.Name)
		if err != nil {
			return err
		}
		if clean == "." && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("snapshot upper layer root entry is not a directory")
		}
		if header.Typeflag == tar.TypeChar && header.Devmajor == 0 && header.Devminor == 0 {
			if clean == "." {
				return fmt.Errorf("snapshot upper layer cannot whiteout its root")
			}
			whiteout := *header
			whiteout.Name = pathpkg.Join(pathpkg.Dir(clean), ".wh."+pathpkg.Base(clean))
			whiteout.Typeflag, whiteout.Mode, whiteout.Size = tar.TypeReg, 0600, 0
			whiteout.Devmajor, whiteout.Devminor, whiteout.Linkname = 0, 0, ""
			whiteout.PAXRecords = withoutOverlayOpaqueXattr(header.PAXRecords)
			whiteout.Xattrs = withoutOverlayOpaqueXattr(header.Xattrs)
			if err := writer.WriteHeader(&whiteout); err != nil {
				return fmt.Errorf("write OCI whiteout for %q: %w", header.Name, err)
			}
			continue
		}
		if header.Typeflag == tar.TypeLink {
			target, targetErr := safeRootfsSnapshotPath(header.Linkname)
			if targetErr != nil {
				return fmt.Errorf("snapshot upper layer hard link %q: %w", header.Name, targetErr)
			}
			if _, ok := materialized[target]; !ok {
				return fmt.Errorf("snapshot upper layer hard link %q targets unavailable entry %q", header.Name, header.Linkname)
			}
			header.Linkname = target
		}
		opaque := rootfsSnapshotDirectoryIsOpaque(header)
		header.Name = clean
		if header.Typeflag == tar.TypeDir && clean != "." {
			header.Name += "/"
		}
		header.PAXRecords = withoutOverlayOpaqueXattr(header.PAXRecords)
		header.Xattrs = withoutOverlayOpaqueXattr(header.Xattrs)
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir, tar.TypeSymlink, tar.TypeLink:
		default:
			return fmt.Errorf("snapshot upper layer contains unsupported special file %q (type %d)", header.Name, header.Typeflag)
		}
		if err := writer.WriteHeader(header); err != nil {
			return fmt.Errorf("write OCI snapshot entry %q: %w", header.Name, err)
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			if _, err := io.Copy(writer, reader); err != nil {
				return fmt.Errorf("write OCI snapshot content %q: %w", header.Name, err)
			}
		}
		materialized[clean] = struct{}{}
		if opaque {
			marker := &tar.Header{
				Name: pathpkg.Join(clean, ".wh..wh..opq"), Typeflag: tar.TypeReg,
				Mode: 0600, Uid: header.Uid, Gid: header.Gid, ModTime: header.ModTime,
			}
			if err := writer.WriteHeader(marker); err != nil {
				return fmt.Errorf("write OCI opaque marker for %q: %w", header.Name, err)
			}
		}
	}
}

func safeRootfsSnapshotPath(name string) (string, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("snapshot upper layer contains unsafe path %q", name)
	}
	return clean, nil
}

func rootfsSnapshotDirectoryIsOpaque(header *tar.Header) bool {
	if header == nil || header.Typeflag != tar.TypeDir {
		return false
	}
	for key, value := range header.PAXRecords {
		if (key == "SCHILY.xattr.trusted.overlay.opaque" || key == "SCHILY.xattr.user.overlay.opaque") && value == "y" {
			return true
		}
	}
	for key, value := range header.Xattrs {
		if (key == "trusted.overlay.opaque" || key == "user.overlay.opaque") && value == "y" {
			return true
		}
	}
	return false
}

func withoutOverlayOpaqueXattr(records map[string]string) map[string]string {
	if len(records) == 0 {
		return nil
	}
	result := make(map[string]string, len(records))
	for key, value := range records {
		if key == "SCHILY.xattr.trusted.overlay.opaque" || key == "SCHILY.xattr.user.overlay.opaque" || key == "trusted.overlay.opaque" || key == "user.overlay.opaque" {
			continue
		}
		result[key] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
