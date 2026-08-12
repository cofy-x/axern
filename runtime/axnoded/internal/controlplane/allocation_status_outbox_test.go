package controlplane

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAllocationStatusOutboxAcknowledgesOnlyExactObservation(t *testing.T) {
	outbox := NewAllocationStatusOutbox(storetest.NewMockStore())
	older := terminalStatusObservation("alloc-1", 1, 17, time.Unix(100, 1).UTC())
	newer := terminalStatusObservation("alloc-1", 2, 42, time.Unix(100, 2).UTC())
	if current, err := outbox.Persist(older); err != nil || !current {
		t.Fatalf("Persist(older) error = %v", err)
	}
	if current, err := outbox.Persist(newer); err != nil || !current {
		t.Fatalf("Persist(newer) error = %v", err)
	}
	if err := outbox.Acknowledge([]*nodev1.AllocationStatusObservation{older}); err != nil {
		t.Fatalf("Acknowledge(older) error = %v", err)
	}

	replayed, err := outbox.Replay()
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].GetExitCode() != 42 {
		t.Fatalf("replayed observations = %#v, want only newer exit", replayed)
	}
}

func TestAllocationStatusOutboxRejectsNonterminalObservation(t *testing.T) {
	outbox := NewAllocationStatusOutbox(storetest.NewMockStore())
	_, err := outbox.Persist(&nodev1.AllocationStatusObservation{
		AllocationID: "alloc-1",
		Attempt:      1,
		Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		ObservedAt:   timestamppb.Now(),
	})
	if err == nil {
		t.Fatal("Persist(nonterminal) error = nil, want rejection")
	}
}

func TestAllocationStatusOutboxKeepsFirstTerminalProofForAttempt(t *testing.T) {
	outbox := NewAllocationStatusOutbox(storetest.NewMockStore())
	first := terminalStatusObservation("alloc-1", 1, 17, time.Unix(100, 1).UTC())
	conflict := terminalStatusObservation("alloc-1", 1, 42, time.Unix(100, 2).UTC())
	if current, err := outbox.Persist(first); err != nil || !current {
		t.Fatalf("Persist(first) = current %v, error %v", current, err)
	}
	if current, err := outbox.Persist(conflict); err != nil || current {
		t.Fatalf("Persist(conflict) = current %v, error %v; want ignored", current, err)
	}
	replayed, err := outbox.Replay()
	if err != nil || len(replayed) != 1 || replayed[0].GetExitCode() != 17 {
		t.Fatalf("Replay() = %#v, error %v; want first terminal proof", replayed, err)
	}
}

func terminalStatusObservation(allocationID string, attempt int64, exitCode int32, observedAt time.Time) *nodev1.AllocationStatusObservation {
	return &nodev1.AllocationStatusObservation{
		AllocationID:  allocationID,
		Attempt:       attempt,
		Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		ExitCode:      exitCode,
		ExitCodeKnown: true,
		ObservedAt:    timestamppb.New(observedAt),
	}
}
