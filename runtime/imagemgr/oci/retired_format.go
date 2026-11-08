package oci

import (
	"encoding/binary"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// These format markers are rejection evidence, never a supported backend.
const (
	retiredLayerAnnotationNydusBootstrap = "containerd.io/snapshot/nydus-bootstrap"
	retiredLayerAnnotationNydusBlob      = "containerd.io/snapshot/nydus-blob"
	retiredMediaTypeNydusBlob            = "application/vnd.oci.image.layer.nydus.blob.v1"
	retiredBootstrapFileNameInLayer      = "image/image.boot"
	retiredBootstrapHeaderSize           = 1024 + 128
)

func hasRetiredImageAnnotations(annotations map[string]string) bool {
	_, bootstrap := annotations[retiredLayerAnnotationNydusBootstrap]
	_, blob := annotations[retiredLayerAnnotationNydusBlob]
	return bootstrap || blob
}

func isRetiredImageDescriptor(descriptor v1.Descriptor) bool {
	return string(descriptor.MediaType) == retiredMediaTypeNydusBlob || hasRetiredImageAnnotations(descriptor.Annotations)
}

// RAFS v6 uses EROFS magic plus its RAFS compatibility bit. An ordinary EROFS
// file is not rejection evidence. Only the bounded format header is inspected.
func isRetiredBootstrap(header []byte) bool {
	if len(header) >= 8 && binary.LittleEndian.Uint32(header[:4]) == 0x52414653 && binary.LittleEndian.Uint32(header[4:8]) == 0x500 {
		return true
	}
	return len(header) >= 1036 && binary.LittleEndian.Uint32(header[1024:1028]) == 0xe0f5e1e2 && binary.LittleEndian.Uint32(header[1032:1036])&0x40000000 != 0
}
