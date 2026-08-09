package verifyutil

import (
	"fmt"
	"path/filepath"
	"strings"

	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

type RootfsSpec struct {
	Type            string
	ImageRef        string
	LocalRootfsPath string
	S3Rootfs        *privatenodev1.S3Rootfs
}

func BuildRootfsSpec(src, localPath, imageURL, endpoint, bucket, object, accessKeyID, accessKeySecret string) (*RootfsSpec, error) {
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
	case "s3":
		if endpoint == "" || bucket == "" || object == "" {
			return nil, fmt.Errorf("s3 rootfs requires endpoint, bucket, and object")
		}
		return &RootfsSpec{
			Type: "s3",
			S3Rootfs: &privatenodev1.S3Rootfs{
				Endpoint:        endpoint,
				Bucket:          bucket,
				Object:          object,
				AccessKeyID:     accessKeyID,
				AccessKeySecret: accessKeySecret,
			},
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
	spec.S3Rootfs = r.S3Rootfs
	switch r.Type {
	case "local":
		spec.LocalityKey = "local:" + filepath.Clean(r.LocalRootfsPath)
	case "image":
		spec.LocalityKey = "image:" + strings.TrimSpace(r.ImageRef)
	case "s3":
		spec.LocalityKey = fmt.Sprintf("s3:%s/%s/%s",
			strings.TrimSpace(r.S3Rootfs.GetEndpoint()),
			strings.TrimSpace(r.S3Rootfs.GetBucket()),
			strings.TrimPrefix(strings.TrimSpace(r.S3Rootfs.GetObject()), "/"),
		)
	}
}
