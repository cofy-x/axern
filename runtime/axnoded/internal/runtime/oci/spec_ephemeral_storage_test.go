package oci

import (
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func TestApplyEphemeralStorageAnnotationUsesPublicIdentity(t *testing.T) {
	ociSpec := &specs.Spec{}
	applyEphemeralStorageAnnotation(ociSpec, &apipb.CreateContainerRequest{
		EphemeralStorageRequestBytes: 64 << 20,
		EphemeralStorageLimitBytes:   128 << 20,
	})

	if got := ociSpec.Annotations["io.axnoded.resource/ephemeral-storage"]; got != `{"request_bytes":67108864,"limit_bytes":134217728}` {
		t.Fatalf("ephemeral-storage annotation = %q", got)
	}
}
