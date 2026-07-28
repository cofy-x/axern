package parse

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestAllocationStatuses(t *testing.T) {
	got, err := AllocationStatuses([]string{"running,failed", "released"})
	if err != nil {
		t.Fatalf("AllocationStatuses returned error: %v", err)
	}
	want := []commonv1.AllocationStatus{
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED,
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

func TestAllocationStatusesRejectsUnknownValue(t *testing.T) {
	if _, err := AllocationStatuses([]string{"wat"}); err == nil {
		t.Fatal("expected error for invalid allocation status")
	} else if !strings.Contains(err.Error(), "reserved, bound") {
		t.Fatalf("error %q does not include valid values", err)
	}
}
