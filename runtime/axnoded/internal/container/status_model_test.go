package container

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func TestEmptyStatusIsUnknown(t *testing.T) {
	if got := (Status{}).State(); got != apipb.ContainerState_CONTAINER_UNKNOWN {
		t.Fatalf("empty status state = %s, want UNKNOWN", got)
	}
}

func TestRuntimeStateDoesNotDependOnOptionalTimestamp(t *testing.T) {
	status := Status{RuntimeState: apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_RUNNING, Pid: 42}
	if got := status.State(); got != apipb.ContainerState_CONTAINER_RUNNING {
		t.Fatalf("explicit runtime state = %s, want RUNNING", got)
	}
}

func TestRuntimeCheckpointRoundTrip(t *testing.T) {
	want := Status{
		RuntimeState: apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_EXITED,
		Pid:          42, StartedAt: "2026-09-13T12:00:00.123456789Z", FinishedAt: "2026-09-13T12:00:01.987654321Z",
		ExitCode: 23, ExitCodeKnown: true, Message: "exited",
	}
	data, err := want.encode()
	if err != nil {
		t.Fatal(err)
	}
	var got Status
	if err := got.decode(data); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("decoded checkpoint = %+v, want %+v", got, want)
	}
}

func TestRuntimeCheckpointRejectsLegacyJSON(t *testing.T) {
	var status Status
	if err := status.decode([]byte(`{"Version":"v2","Status":{"Pid":42}}`)); err == nil {
		t.Fatal("legacy JSON status was accepted")
	}
}
