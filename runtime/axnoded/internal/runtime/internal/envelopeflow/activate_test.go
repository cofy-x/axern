package envelopeflow

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestActivateExecutionEnvelopeWaitsForSandboxd(t *testing.T) {
	var gotBundlePath string
	var gotMeta *apipb.ContainerMetadata
	ready := false
	meta := &apipb.ContainerMetadata{
		ID: "axctl-prewarm",
	}
	activated, err := Activate(
		context.Background(),
		&contract.ExecutionEnvelope{ContainerID: "axctl-prewarm", BundlePath: "/tmp/bundle", Metadata: meta},
		contract.HandlerOptions{ContainerID: "axctl-prewarm"},
		func(context.Context, string) error { return nil },
		func(string) error { return nil },
		func(context.Context, string) error { return nil },
		func(_ context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
			gotBundlePath = bundlePath
			gotMeta = meta
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
		t.Fatalf("Activate() error = %v", err)
	}
	if activated != meta || gotMeta != meta || gotBundlePath != "/tmp/bundle" {
		t.Fatalf("activated = %#v, gotMeta = %#v, gotBundlePath = %q", activated, gotMeta, gotBundlePath)
	}
}
