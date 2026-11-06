package oci

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/cofy-x/axern/runtime/imagemgr/nydus"
)

// ErrNydusImage means content needs the Nydus backend and cannot be extracted as
// ordinary OCI layers. Callers must not retry it through another OCI route.
var ErrNydusImage = errors.New("Nydus image is not supported by OCI extraction")

func validateOCIImage(img v1.Image) error {
	detected, err := nydus.IsNydusImage(img)
	if err != nil {
		return fmt.Errorf("inspect OCI image format: %w", err)
	}
	if detected {
		return ErrNydusImage
	}
	return nil
}

func validateOCIArchiveDescriptor(descriptor tarball.Descriptor) error {
	// ImageFromPath reconstructs a Docker manifest and may normalize descriptor
	// metadata. Inspect retained source descriptors before that conversion.
	for _, source := range descriptor.LayerSources {
		if nydus.IsNydusDescriptor(source) {
			return ErrNydusImage
		}
	}
	return nil
}

func loadOCIArchiveImage(path string) (v1.Image, error) {
	manifest, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(path) })
	if err != nil {
		return nil, fmt.Errorf("invalid Docker archive: %w", err)
	}
	if len(manifest) != 1 {
		return nil, fmt.Errorf("Docker archive must contain exactly one image, found %d", len(manifest))
	}
	if err := validateOCIArchiveDescriptor(manifest[0]); err != nil {
		return nil, err
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, fmt.Errorf("load Docker archive image: %w", err)
	}
	if err := validateOCIImage(img); err != nil {
		return nil, err
	}
	return img, nil
}

func validateImportBootstrapLayer(ctx context.Context, layer v1.Layer) error {
	stream, err := layer.Uncompressed()
	if err != nil {
		return fmt.Errorf("open imported bootstrap layer: %w", err)
	}
	defer stream.Close()
	reader := tar.NewReader(stream)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect imported bootstrap layer: %w", err)
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return err
		}
		if name == nydus.BootstrapFileNameInLayer && (header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA) {
			if err := validateBootstrapContent(reader); err != nil {
				return err
			}
		}
	}
}

func validateBootstrapContent(reader io.Reader) error {
	header, err := io.ReadAll(io.LimitReader(reader, nydus.BootstrapHeaderSize))
	if err != nil {
		return fmt.Errorf("inspect bootstrap header: %w", err)
	}
	if nydus.IsNydusBootstrap(header) {
		return fmt.Errorf("%w: RAFS bootstrap at %s", ErrNydusImage, nydus.BootstrapFileNameInLayer)
	}
	return nil
}

func validateExtractedLayerFormat(layerPath string) error {
	// Check the fixed bootstrap path after extraction and before cache reuse.
	// This also protects layers extracted by an older process without scanning
	// or downloading ordinary OCI layers a second time. Never follow a layer's
	// symlink outside its root, or interpret a similarly named ordinary file as RAFS.
	root, err := os.OpenRoot(layerPath)
	if err != nil {
		return fmt.Errorf("open extracted layer: %w", err)
	}
	defer root.Close()
	info, err := root.Lstat("image")
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect layer bootstrap directory: %w", err)
	}
	info, err = root.Lstat(nydus.BootstrapFileNameInLayer)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect layer bootstrap file: %w", err)
	}
	file, err := root.Open(nydus.BootstrapFileNameInLayer)
	if err != nil {
		return fmt.Errorf("open layer bootstrap file: %w", err)
	}
	defer file.Close()
	return validateBootstrapContent(file)
}
