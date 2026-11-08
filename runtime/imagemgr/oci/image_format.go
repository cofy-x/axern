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
)

// ErrNydusImage rejects retired content before extraction or cache publication.
var ErrNydusImage = errors.New("Nydus image is not supported by OCI extraction")

func validateOCIImage(img v1.Image) error {
	manifest, err := img.Manifest()
	if err != nil {
		return fmt.Errorf("inspect OCI image format: %w", err)
	}
	if hasRetiredImageAnnotations(manifest.Annotations) {
		return ErrNydusImage
	}
	for _, layer := range manifest.Layers {
		if isRetiredImageDescriptor(layer) {
			return ErrNydusImage
		}
	}
	return nil
}

func validateOCIArchiveDescriptor(descriptor tarball.Descriptor) error {
	// ImageFromPath reconstructs a Docker manifest and may normalize descriptor
	// metadata. Inspect retained source descriptors before that conversion.
	for _, source := range descriptor.LayerSources {
		if isRetiredImageDescriptor(source) {
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
		if name == retiredBootstrapFileNameInLayer && (header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA) {
			if err := validateBootstrapContent(reader); err != nil {
				return err
			}
		}
	}
}

func validateBootstrapContent(reader io.Reader) error {
	header, err := io.ReadAll(io.LimitReader(reader, retiredBootstrapHeaderSize))
	if err != nil {
		return fmt.Errorf("inspect bootstrap header: %w", err)
	}
	if isRetiredBootstrap(header) {
		return fmt.Errorf("%w: RAFS bootstrap at %s", ErrNydusImage, retiredBootstrapFileNameInLayer)
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
	info, err = root.Lstat(retiredBootstrapFileNameInLayer)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect layer bootstrap file: %w", err)
	}
	file, err := root.Open(retiredBootstrapFileNameInLayer)
	if err != nil {
		return fmt.Errorf("open layer bootstrap file: %w", err)
	}
	defer file.Close()
	return validateBootstrapContent(file)
}
