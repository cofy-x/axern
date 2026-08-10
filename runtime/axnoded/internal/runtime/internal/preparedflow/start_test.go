package preparedflow

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestStartWaitsForSandboxdBeforeRuntimeVerification(t *testing.T) {
	var gotBundlePath string
	var gotMeta *apipb.ContainerMetadata
	ready := false
	meta := &apipb.ContainerMetadata{ID: "allocation-a"}
	started, err := Start(
		t.Context(),
		&contract.PreparedContainer{ContainerID: "allocation-a", BundlePath: "/tmp/bundle", Metadata: meta},
		contract.HandlerOptions{ContainerID: "allocation-a"},
		func(context.Context, string) error { return nil },
		func(string) error { return nil },
		func(context.Context, string) error { return nil },
		func(_ context.Context, bundlePath string, metadata *apipb.ContainerMetadata) error {
			gotBundlePath = bundlePath
			gotMeta = metadata
			ready = true
			return nil
		},
		func(context.Context) error {
			if !ready {
				t.Fatal("runtime verification ran before sandbox readiness")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started != meta || gotMeta != meta || gotBundlePath != "/tmp/bundle" {
		t.Fatalf("started = %#v, gotMeta = %#v, gotBundlePath = %q", started, gotMeta, gotBundlePath)
	}
}
