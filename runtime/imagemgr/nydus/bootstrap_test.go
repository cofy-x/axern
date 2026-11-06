package nydus

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type manifestImage struct {
	v1.Image
	manifest *v1.Manifest
	err      error
}

func (image manifestImage) Manifest() (*v1.Manifest, error) { return image.manifest, image.err }

func TestIsNydusImageDetectsManifestAnnotationsWithoutLayers(t *testing.T) {
	for _, annotation := range []string{LayerAnnotationNydusBootstrap, LayerAnnotationNydusBlob} {
		t.Run(annotation, func(t *testing.T) {
			image := manifestImage{manifest: &v1.Manifest{Annotations: map[string]string{annotation: "true"}}}
			if detected, err := IsNydusImage(image); err != nil || !detected {
				t.Fatalf("IsNydusImage() = (%t, %v), want true", detected, err)
			}
		})
	}
}

func TestIsNydusImagePreservesManifestError(t *testing.T) {
	want := errors.New("manifest unavailable")
	if _, err := IsNydusImage(manifestImage{err: want}); !errors.Is(err, want) {
		t.Fatalf("IsNydusImage() error = %v, want %v", err, want)
	}
}

func TestIsNydusBootstrapRecognizesRAFSHeaders(t *testing.T) {
	v5 := make([]byte, 8192)
	binary.LittleEndian.PutUint32(v5[0:4], 0x52414653)
	binary.LittleEndian.PutUint32(v5[4:8], 0x500)
	binary.LittleEndian.PutUint32(v5[8:12], 8192)
	binary.LittleEndian.PutUint32(v5[12:16], 4096)
	v6 := make([]byte, 4096)
	binary.LittleEndian.PutUint32(v6[1024:1028], 0xe0f5e1e2)
	binary.LittleEndian.PutUint32(v6[1032:1036], 0x40000000)
	v6[1036] = 12
	erofs := append([]byte(nil), v6...)
	binary.LittleEndian.PutUint32(erofs[1032:1036], 0)
	wrongVersion := append([]byte(nil), v5...)
	binary.LittleEndian.PutUint32(wrongVersion[4:8], 1)

	for _, test := range []struct {
		name string
		data []byte
		want bool
	}{
		{"rafs-v5", v5[:BootstrapHeaderSize], true},
		{"rafs-v6", v6[:BootstrapHeaderSize], true},
		{"damaged-v5-still-has-format-signature", v5[:8], true},
		{"ordinary-erofs", erofs, false},
		{"wrong-v5-version", wrongVersion, false},
		{"ordinary-file", []byte("this is an ordinary image.boot file"), false},
		{"truncated-v6-signature", v6[:1035], false},
		{"empty", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNydusBootstrap(test.data); got != test.want {
				t.Fatalf("IsNydusBootstrap() = %t, want %t", got, test.want)
			}
		})
	}
}

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
