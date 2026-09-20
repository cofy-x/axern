package environment

import (
	"context"
	"fmt"
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestResolveSpecImageSource(t *testing.T) {
	spec, resolved, err := ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{
		Namespace: "prod",
		Image: &environmentv1.EnvironmentImageSource{
			Ref:            "docker.io/library/nginx:1.27",
			RootfsReadonly: true,
		},
	}, fakeImageResolver{}, nil)
	if err != nil {
		t.Fatalf("ResolveSpec(image) error = %v", err)
	}
	if spec.GetImage().GetRef() != "index.docker.io/library/nginx:1.27" {
		t.Fatalf("normalized image ref = %q", spec.GetImage().GetRef())
	}
	if resolved.GetImageDescriptor().GetDigest() == "" {
		t.Fatal("resolved image descriptor digest is empty")
	}
	if !resolved.GetRootfsReadonly() {
		t.Fatalf("resolved image specification = %+v, want rootfs readonly propagated", resolved)
	}
}

func TestResolveSpecRequiresImage(t *testing.T) {
	_, _, err := ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{}, fakeImageResolver{}, nil)
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing image code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

type fakeImageResolver struct{}

func (fakeImageResolver) Resolve(_ context.Context, imageRef string, _ ResolveOptions) (*ResolvedImage, error) {
	if imageRef != "docker.io/library/nginx:1.27" {
		return nil, fmt.Errorf("unexpected image ref %q", imageRef)
	}
	return &ResolvedImage{
		Ref: "index.docker.io/library/nginx:1.27",
		Descriptor: &environmentv1.OciImageDescriptor{
			Digest:      "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			MediaType:   "application/vnd.oci.image.manifest.v1+json",
			SizeBytes:   1234,
			Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.27"},
		},
	}, nil
}
