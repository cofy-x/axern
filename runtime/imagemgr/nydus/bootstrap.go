package nydus

import (
	"archive/tar"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	// Standard Nydus annotations and media types
	// Reference: github.com/containerd/nydus-snapshotter/pkg/converter/constant.go
	// Reference: github.com/containerd/nydus-snapshotter/pkg/label/label.go

	// Layer annotations
	LayerAnnotationNydusBootstrap = "containerd.io/snapshot/nydus-bootstrap"
	LayerAnnotationNydusBlob      = "containerd.io/snapshot/nydus-blob"

	// Media types
	MediaTypeNydusBlob = "application/vnd.oci.image.layer.nydus.blob.v1"

	// Bootstrap file name in layer tar
	BootstrapFileNameInLayer = "image/image.boot"

	// BootstrapHeaderSize covers the RAFS v5 header and the RAFS v6 superblock
	// at byte 1024. Format recognition never needs to read the complete bootstrap.
	BootstrapHeaderSize = 1024 + 128
)

type bootstrapExtractionTimings struct {
	ListLayers           time.Duration
	PrepareOutput        time.Duration
	OpenBootstrapStream  time.Duration
	ScanBootstrapArchive time.Duration
	CopyBootstrapFile    time.Duration
}

type bootstrapExtractionResult struct {
	Path    string
	Timings bootstrapExtractionTimings
}

// IsNydusImage checks if an image is in Nydus format
// Reference: github.com/containerd/nydus-snapshotter/pkg/converter/convert_unix.go::isNydusImage
func IsNydusImage(img v1.Image) (bool, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return false, fmt.Errorf("failed to get manifest: %w", err)
	}

	// Nydus conversion tools can mark the image through layer media types,
	// layer annotations, or manifest annotations depending on output schema.
	for _, layer := range manifest.Layers {
		if IsNydusDescriptor(layer) {
			return true, nil
		}
	}
	return hasNydusAnnotations(manifest.Annotations), nil
}

// IsNydusDescriptor recognizes image-format markers on a layer descriptor,
// including descriptors retained by Docker archives in LayerSources.
func IsNydusDescriptor(descriptor v1.Descriptor) bool {
	return string(descriptor.MediaType) == MediaTypeNydusBlob || hasNydusAnnotations(descriptor.Annotations)
}

func hasNydusAnnotations(annotations map[string]string) bool {
	_, bootstrap := annotations[LayerAnnotationNydusBootstrap]
	_, blob := annotations[LayerAnnotationNydusBlob]
	return bootstrap || blob
}

// IsNydusBootstrap recognizes RAFS headers when an archive has lost its image
// annotations. This identifies the format, not filesystem validity: damaged
// Nydus content must not become an ordinary OCI rootfs. RAFS v6 shares EROFS's
// magic, so its RAFS compatibility bit is required to distinguish ordinary EROFS.
// Reference: nydus/rafs/src/metadata/layout/{v5,v6}.rs.
func IsNydusBootstrap(header []byte) bool {
	if len(header) >= 8 && binary.LittleEndian.Uint32(header[0:4]) == 0x52414653 &&
		binary.LittleEndian.Uint32(header[4:8]) == 0x500 {
		return true
	}
	return len(header) >= 1036 && binary.LittleEndian.Uint32(header[1024:1028]) == 0xe0f5e1e2 &&
		binary.LittleEndian.Uint32(header[1032:1036])&0x40000000 != 0
}

// ExtractBootstrap extracts the Nydus bootstrap from the image
// In Nydus images, the bootstrap is always in the last layer
func ExtractBootstrap(_ context.Context, img v1.Image, outputDir string) (string, error) {
	result, err := extractBootstrap(img, outputDir)
	return result.Path, err
}

func extractBootstrap(img v1.Image, outputDir string) (bootstrapExtractionResult, error) {
	var result bootstrapExtractionResult

	stageStart := time.Now()
	layers, err := img.Layers()
	result.Timings.ListLayers = time.Since(stageStart)
	if err != nil {
		return result, fmt.Errorf("failed to get layers: %w", err)
	}

	if len(layers) == 0 {
		return result, fmt.Errorf("image has no layers")
	}

	stageStart = time.Now()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return result, fmt.Errorf("failed to create output directory: %w", err)
	}
	result.Timings.PrepareOutput = time.Since(stageStart)

	lastLayer := layers[len(layers)-1]
	result.Path, err = extractBootstrapFromLayer(lastLayer, outputDir, &result.Timings)
	if err != nil {
		return result, fmt.Errorf("failed to extract bootstrap from last layer: %w", err)
	}

	return result, nil
}

func extractBootstrapFromLayer(layer v1.Layer, outputDir string, timings *bootstrapExtractionTimings) (string, error) {
	stageStart := time.Now()
	rc, err := layer.Uncompressed()
	timings.OpenBootstrapStream = time.Since(stageStart)
	if err != nil {
		return "", fmt.Errorf("failed to decompress layer: %w", err)
	}
	defer rc.Close()

	stageStart = time.Now()
	tr := tar.NewReader(rc)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read tar: %w", err)
		}

		// Bootstrap must be at the standard path
		if header.Name == BootstrapFileNameInLayer {
			timings.ScanBootstrapArchive = time.Since(stageStart)

			stageStart = time.Now()
			outputPath := filepath.Join(outputDir, "bootstrap")
			if err := extractFile(tr, outputPath); err != nil {
				return "", err
			}
			timings.CopyBootstrapFile = time.Since(stageStart)

			return outputPath, nil
		}
	}

	return "", fmt.Errorf("bootstrap file not found at %s", BootstrapFileNameInLayer)
}

func extractFile(tr *tar.Reader, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, tr); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
