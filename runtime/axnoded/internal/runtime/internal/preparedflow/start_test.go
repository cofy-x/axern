package preparedflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestStartWaitsForSandboxdBeforeRuntimeVerification(t *testing.T) {
	var gotBundlePath string
	var gotMeta *apipb.ContainerMetadata
	ready := false
	meta := &apipb.ContainerMetadata{}
	started, err := Start(
		t.Context(),
		&contract.PreparedContainer{ContainerID: "allocation-a", BundlePath: "/tmp/bundle", Metadata: meta},
		contract.HandlerOptions{ContainerID: "allocation-a"},
		func(context.Context, string) error { return nil },
		func(string) error { return nil },
		func(context.Context, string) error { return nil },
		nil,
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

func TestStartFencesSandboxReadinessAfterExitMonitor(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "failed"}[failure], func(t *testing.T) {
			var calls []string
			gateErr := errors.New("task enforcement readback failed")
			_, err := Start(t.Context(), &contract.PreparedContainer{ContainerID: "allocation", Metadata: &apipb.ContainerMetadata{}}, contract.HandlerOptions{},
				func(context.Context, string) error { calls = append(calls, "start"); return nil },
				func(string) error { calls = append(calls, "exit monitor"); return nil },
				func(context.Context, string) error { calls = append(calls, "running"); return nil },
				func(context.Context) error {
					calls = append(calls, "task gate")
					if failure {
						return gateErr
					}
					return nil
				},
				func(context.Context, string, *apipb.ContainerMetadata) error {
					calls = append(calls, "ready")
					return nil
				},
				nil,
			)
			want := []string{"start", "exit monitor", "running", "task gate"}
			if !failure {
				want = append(want, "ready")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls = %v, want %v", calls, want)
			}
			if failure && !errors.Is(err, gateErr) || !failure && err != nil {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
