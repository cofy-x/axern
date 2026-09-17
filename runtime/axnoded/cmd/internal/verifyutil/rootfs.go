package verifyutil

import (
	"fmt"
	"path/filepath"
	"strings"

	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
)

type RootfsSpec struct {
	Type            string
	ImageRef        string
	LocalRootfsPath string
}

func BuildRootfsSpec(src, localPath, imageURL string) (*RootfsSpec, error) {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "", "local":
		return &RootfsSpec{
			Type:            "local",
			LocalRootfsPath: localPath,
		}, nil
	case "image":
		if imageURL == "" {
			return nil, fmt.Errorf("image rootfs requires image-url")
		}
		return &RootfsSpec{
			Type:     "image",
			ImageRef: imageURL,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported rootfs source %q", src)
	}
}

func (r *RootfsSpec) Apply(spec *privatenodev1.ResolvedExecutionConfig) {
	if r == nil || spec == nil {
		return
	}
	spec.ImageDescriptor = r.ImageRef
	spec.ImageDigest = r.ImageRef
	spec.LocalRootfsPath = r.LocalRootfsPath
	switch r.Type {
	case "local":
		spec.LocalityKey = "local:" + filepath.Clean(r.LocalRootfsPath)
	case "image":
		spec.LocalityKey = "image:" + strings.TrimSpace(r.ImageRef)
	}
}
