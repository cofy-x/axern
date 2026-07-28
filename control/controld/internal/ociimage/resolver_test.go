package ociimage

import (
	"context"
	"errors"
	"strings"
	"testing"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	"github.com/google/go-containerregistry/pkg/name"
)

func TestClassifyResolveError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		usedCredential bool
		wantContains   string
	}{
		{
			name:           "private registry without credential",
			err:            errors.New("GET https://registry.example.com/v2/: UNAUTHORIZED"),
			wantContains:   "provide a registry credential",
			usedCredential: false,
		},
		{
			name:           "private registry with bad credential",
			err:            errors.New("DENIED: requested access to the resource is denied"),
			wantContains:   "check the referenced docker-config-json secret",
			usedCredential: true,
		},
		{
			name:           "missing image",
			err:            errors.New("MANIFEST_UNKNOWN"),
			wantContains:   "image or tag was not found",
			usedCredential: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyResolveError("registry.example.com/team/api:1.0", "registry.example.com", tc.usedCredential, tc.err)
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Fatalf("error %q does not contain %q", err, tc.wantContains)
			}
		})
	}
}

func TestResolveDigestReferenceDoesNotContactRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	const ref = "python@sha256:5072b08ad74609c5329ab4085a96dfa873de565fb4751a4cfcd7dcc427661df0"
	const normalized = "index.docker.io/library/python@sha256:5072b08ad74609c5329ab4085a96dfa873de565fb4751a4cfcd7dcc427661df0"
	resolved, err := NewResolver().Resolve(ctx, ref, environmentkernel.ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Ref != normalized {
		t.Fatalf("Ref = %q, want %q", resolved.Ref, normalized)
	}
	if got := resolved.Descriptor.GetDigest(); got != "sha256:5072b08ad74609c5329ab4085a96dfa873de565fb4751a4cfcd7dcc427661df0" {
		t.Fatalf("digest = %q", got)
	}
}

func TestUseHTTPForConfiguredInsecureRegistries(t *testing.T) {
	t.Setenv("CONTROLD_INSECURE_REGISTRIES", "host.docker.internal:5001, http://localhost:5001/")

	tests := []struct {
		name     string
		imageRef string
		want     bool
	}{
		{
			name:     "configured host docker registry",
			imageRef: "host.docker.internal:5001/axern/python311-runtime:dev",
			want:     true,
		},
		{
			name:     "configured localhost registry with scheme",
			imageRef: "http://localhost:5001/axern/python311-runtime:dev",
			want:     true,
		},
		{
			name:     "docker hub short ref remains secure",
			imageRef: "python:3.12-slim",
			want:     false,
		},
		{
			name:     "unconfigured registry remains secure",
			imageRef: "registry.example.com/team/api:1.0",
			want:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := useHTTPFor(tc.imageRef); got != tc.want {
				t.Fatalf("useHTTPFor(%q) = %v, want %v", tc.imageRef, got, tc.want)
			}
		})
	}
}

func TestUseHTTPForIgnoresImagemgrEnv(t *testing.T) {
	t.Setenv("IMAGEMGR_INSECURE_REGISTRIES", "localhost:5001")

	if useHTTPFor("localhost:5001/axern/python311-runtime:dev") {
		t.Fatal("useHTTPFor(local registry) = true, want false without CONTROLD_INSECURE_REGISTRIES")
	}
}

func TestRegistryHost(t *testing.T) {
	tests := []struct {
		imageRef string
		want     string
	}{
		{imageRef: "python:3.12-slim", want: name.DefaultRegistry},
		{imageRef: "library/python:3.12-slim", want: name.DefaultRegistry},
		{imageRef: "localhost:5001/axern/python311-runtime:dev", want: "localhost:5001"},
		{imageRef: "https://host.docker.internal:5001/axern/python311-runtime:dev", want: "host.docker.internal:5001"},
	}
	for _, tc := range tests {
		t.Run(tc.imageRef, func(t *testing.T) {
			if got := registryHost(tc.imageRef); got != tc.want {
				t.Fatalf("registryHost(%q) = %q, want %q", tc.imageRef, got, tc.want)
			}
		})
	}
}
