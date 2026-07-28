package controldtest

import (
	"context"
	"fmt"
	"strings"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

type FakeImageResolver struct {
	Images      map[string]*environmentkernel.ResolvedImage
	LastRef     string
	LastOptions environmentkernel.ResolveOptions
}

func NewFakeImageResolver() *FakeImageResolver {
	return &FakeImageResolver{
		Images: map[string]*environmentkernel.ResolvedImage{
			"docker.io/library/nginx:1.27": {
				Ref: "index.docker.io/library/nginx:1.27",
				Descriptor: &catalogv1.OciImageDescriptor{
					Digest:      "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					MediaType:   "application/vnd.oci.image.manifest.v1+json",
					SizeBytes:   1234,
					Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.27"},
				},
			},
			"docker.io/library/nginx:1.28": {
				Ref: "index.docker.io/library/nginx:1.28",
				Descriptor: &catalogv1.OciImageDescriptor{
					Digest:      "sha256:2222222222222222222222222222222222222222222222222222222222222222",
					MediaType:   "application/vnd.oci.image.manifest.v1+json",
					SizeBytes:   1234,
					Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.28"},
				},
			},
			"docker.io/library/alpine:3.20": {
				Ref: "index.docker.io/library/alpine:3.20",
				Descriptor: &catalogv1.OciImageDescriptor{
					Digest:      "sha256:3333333333333333333333333333333333333333333333333333333333333333",
					MediaType:   "application/vnd.oci.image.manifest.v1+json",
					SizeBytes:   456,
					Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/alpine:3.20"},
				},
			},
			"ghcr.io/acme/private-app:v2": {
				Ref: "ghcr.io/acme/private-app:v2",
				Descriptor: &catalogv1.OciImageDescriptor{
					Digest:      "sha256:4444444444444444444444444444444444444444444444444444444444444444",
					MediaType:   "application/vnd.oci.image.manifest.v1+json",
					SizeBytes:   789,
					Annotations: map[string]string{"org.opencontainers.image.ref.name": "ghcr.io/acme/private-app:v2"},
				},
			},
		},
	}
}

func (r *FakeImageResolver) Resolve(_ context.Context, imageRef string, opts environmentkernel.ResolveOptions) (*environmentkernel.ResolvedImage, error) {
	if r == nil {
		return nil, fmt.Errorf("fake image resolver is nil")
	}
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	r.LastRef = imageRef
	r.LastOptions = opts
	if resolved, ok := r.Images[imageRef]; ok && resolved != nil {
		return cloneResolvedImage(resolved), nil
	}
	return nil, fmt.Errorf("image %q not found", imageRef)
}

func cloneResolvedImage(in *environmentkernel.ResolvedImage) *environmentkernel.ResolvedImage {
	if in == nil {
		return nil
	}
	out := &environmentkernel.ResolvedImage{Ref: in.Ref}
	if in.Descriptor != nil {
		out.Descriptor = &catalogv1.OciImageDescriptor{
			Digest:      in.Descriptor.GetDigest(),
			MediaType:   in.Descriptor.GetMediaType(),
			SizeBytes:   in.Descriptor.GetSizeBytes(),
			Annotations: cloneStringMap(in.Descriptor.GetAnnotations()),
		}
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
