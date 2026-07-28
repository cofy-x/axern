package nydus

import (
	"archive/tar"
	"context"
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

	layers := manifest.Layers
	if len(layers) == 0 {
		return false, nil
	}

	// Nydus conversion tools can mark the image through layer media types,
	// layer annotations, or manifest annotations depending on output schema.
	for _, layer := range layers {
		if string(layer.MediaType) == MediaTypeNydusBlob {
			return true, nil
		}
		if layer.Annotations != nil {
			if _, ok := layer.Annotations[LayerAnnotationNydusBootstrap]; ok {
				return true, nil
			}
			if _, ok := layer.Annotations[LayerAnnotationNydusBlob]; ok {
				return true, nil
			}
		}
	}
	if manifest.Annotations != nil {
		if _, ok := manifest.Annotations[LayerAnnotationNydusBootstrap]; ok {
			return true, nil
		}
		if _, ok := manifest.Annotations[LayerAnnotationNydusBlob]; ok {
			return true, nil
		}
	}

	return false, nil
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
