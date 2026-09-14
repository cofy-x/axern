package environment

import (
	"context"
	"fmt"
	"testing"

	"github.com/cofy-x/axern/control/controld/internal/environmenttemplate"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestResolveSpecTemplateSource(t *testing.T) {
	templateStore := environmenttemplate.NewStore(nil)
	spec, template, err := ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{
		Namespace:  "default",
		TemplateID: "python311",
	}, templateStore, nil, nil)
	if err != nil {
		t.Fatalf("ResolveSpec(template) error = %v", err)
	}
	if spec.GetTemplateID() != "python311" || spec.GetTemplateVersion() == "" {
		t.Fatalf("normalized template spec = %+v, want template id and resolved version", spec)
	}
	if spec.GetImage() != nil {
		t.Fatalf("normalized template spec unexpectedly had image source: %+v", spec.GetImage())
	}
	if template == nil || template.GetImageDescriptor().GetDigest() == "" {
		t.Fatalf("resolved template = %+v, want digest-pinned template", template)
	}
}

func TestResolveSpecImageSource(t *testing.T) {
	spec, template, err := ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{
		Namespace: "prod",
		Image: &environmentv1.EnvironmentImageSource{
			Ref:            "docker.io/library/nginx:1.27",
			RootfsReadonly: true,
		},
	}, environmenttemplate.NewStore(nil), fakeImageResolver{}, nil)
	if err != nil {
		t.Fatalf("ResolveSpec(image) error = %v", err)
	}
	if spec.GetTemplateID() != "" || spec.GetTemplateVersion() != "" {
		t.Fatalf("normalized image spec unexpectedly had template fields: %+v", spec)
	}
	if spec.GetImage().GetDigest() == "" {
		t.Fatal("normalized image spec digest = empty, want resolved digest")
	}
	if template.GetImageDescriptor().GetDigest() != spec.GetImage().GetDigest() {
		t.Fatalf("template digest = %q, want %q", template.GetImageDescriptor().GetDigest(), spec.GetImage().GetDigest())
	}
	if !template.GetRootfsReadonly() {
		t.Fatalf("synthesized image template = %+v, want rootfs readonly propagated", template)
	}
}

func TestResolveSpecRejectsInvalidImageCombinations(t *testing.T) {
	_, _, err := ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{
		TemplateID: "python311",
		Image:      &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"},
	}, environmenttemplate.NewStore(nil), fakeImageResolver{}, nil)
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("mixed source code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}

	_, _, err = ResolveSpec(context.Background(), &environmentv1.EnvironmentSpec{
		Image: &environmentv1.EnvironmentImageSource{
			Ref:    "docker.io/library/nginx:1.27",
			Digest: "sha256:client-supplied",
		},
	}, environmenttemplate.NewStore(nil), fakeImageResolver{}, nil)
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("client digest code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
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
