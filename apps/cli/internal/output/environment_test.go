package output

import (
	"strings"
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func TestRenderEnvironmentImageBacked(t *testing.T) {
	var b strings.Builder
	RenderEnvironment(&b, &environmentv1.Environment{
		ID:        "env-1",
		Namespace: "default",
		Spec: &environmentv1.EnvironmentSpec{
			Image: &environmentv1.EnvironmentImageSource{
				Ref:                  "index.docker.io/library/nginx:1.27",
				Digest:               "sha256:abc",
				RegistryCredentialID: "sec-regcred",
				RootfsReadonly:       true,
			},
		},
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest:      "sha256:abc",
				Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.27"},
			},
		},
	})
	out := b.String()
	for _, want := range []string{
		"ID: env-1",
		"Source: image",
		"Image Ref: index.docker.io/library/nginx:1.27",
		"Resolved Digest: sha256:abc",
		"Registry Credential ID: sec-regcred",
		"Normalized Image Ref: index.docker.io/library/nginx:1.27",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderEnvironmentTable(t *testing.T) {
	var b strings.Builder
	RenderEnvironmentTable(&b, []*environmentv1.Environment{
		{
			ID: "env-template",
			Spec: &environmentv1.EnvironmentSpec{
				TemplateID:      "python311",
				TemplateVersion: "sha256:template",
			},
		},
		{
			ID: "env-image",
			Spec: &environmentv1.EnvironmentSpec{
				Image: &environmentv1.EnvironmentImageSource{
					Ref:    "index.docker.io/library/nginx:1.27",
					Digest: "sha256:image",
				},
			},
		},
	})
	out := b.String()
	for _, want := range []string{"env-template", "template", "python311", "env-image", "image", "index.docker.io/library/nginx:1.27"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}
