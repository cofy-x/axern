package nydus

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestExtractBootstrapReturnsStageTimings(t *testing.T) {
	const bootstrapContent = "nydus-bootstrap"

	var layerData bytes.Buffer
	tw := tar.NewWriter(&layerData)
	if err := tw.WriteHeader(&tar.Header{Name: BootstrapFileNameInLayer, Mode: 0644, Size: int64(len(bootstrapContent))}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tw.Write([]byte(bootstrapContent)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	layer := static.NewLayer(layerData.Bytes(), types.OCIUncompressedLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("mutate.AppendLayers() error = %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "daemon")
	result, err := extractBootstrap(img, outputDir)
	if err != nil {
		t.Fatalf("extractBootstrap() error = %v", err)
	}
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != bootstrapContent {
		t.Fatalf("bootstrap content = %q, want %q", content, bootstrapContent)
	}
	if result.Timings.ListLayers <= 0 || result.Timings.OpenBootstrapStream <= 0 || result.Timings.CopyBootstrapFile <= 0 {
		t.Fatalf("incomplete extraction timings: %+v", result.Timings)
	}
}

func TestIsNydusImageDetectsNydusBlobMediaType(t *testing.T) {
	layer := static.NewLayer([]byte("nydus blob"), types.MediaType(MediaTypeNydusBlob))
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer, MediaType: types.MediaType(MediaTypeNydusBlob)})
	if err != nil {
		t.Fatalf("mutate.Append() error = %v", err)
	}

	isNydus, err := IsNydusImage(img)
	if err != nil {
		t.Fatalf("IsNydusImage() error = %v", err)
	}
	if !isNydus {
		t.Fatalf("IsNydusImage() = false, want true")
	}
}

func TestIsNydusImageDetectsLayerAnnotations(t *testing.T) {
	layer := static.NewLayer([]byte("bootstrap"), types.DockerLayer)
	img, err := mutate.Append(empty.Image, mutate.Addendum{
		Layer:     layer,
		MediaType: types.DockerLayer,
		Annotations: map[string]string{
			LayerAnnotationNydusBootstrap: "true",
		},
	})
	if err != nil {
		t.Fatalf("mutate.Append() error = %v", err)
	}

	isNydus, err := IsNydusImage(img)
	if err != nil {
		t.Fatalf("IsNydusImage() error = %v", err)
	}
	if !isNydus {
		t.Fatalf("IsNydusImage() = false, want true")
	}
}

func TestIsNydusImageRejectsPlainOCIImage(t *testing.T) {
	layer := static.NewLayer([]byte("plain layer"), types.DockerLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("mutate.AppendLayers() error = %v", err)
	}

	isNydus, err := IsNydusImage(img)
	if err != nil {
		t.Fatalf("IsNydusImage() error = %v", err)
	}
	if isNydus {
		t.Fatalf("IsNydusImage() = true, want false")
	}
}
