package parse

import (
	"strings"
	"testing"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestRunStatuses(t *testing.T) {
	got, err := RunStatuses([]string{"running,failed", "canceled"})
	if err != nil {
		t.Fatalf("RunStatuses returned error: %v", err)
	}
	want := []runv1.RunStatus{
		runv1.RunStatus_RUN_STATUS_RUNNING,
		runv1.RunStatus_RUN_STATUS_FAILED,
		runv1.RunStatus_RUN_STATUS_CANCELLED,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statuses, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRunStatusesRejectsUnknownValue(t *testing.T) {
	if _, err := RunStatuses([]string{"wat"}); err == nil {
		t.Fatal("expected error for invalid run status")
	} else if !strings.Contains(err.Error(), "queued, placed") {
		t.Fatalf("error %q does not include valid values", err)
	}
}
