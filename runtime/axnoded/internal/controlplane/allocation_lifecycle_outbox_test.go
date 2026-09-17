package controlplane

import (
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAllocationLifecycleOutboxAcknowledgesOnlyExactObservation(t *testing.T) {
	outbox := NewAllocationLifecycleOutbox(storetest.NewMockStore())
	current := terminalStatusObservation("alloc-1", 42, time.Unix(100, 2).UTC())
	nonmatching := terminalStatusObservation("alloc-1", 17, time.Unix(100, 1).UTC())
	if persisted, err := outbox.Persist(current); err != nil || !persisted {
		t.Fatalf("Persist(current) error = %v", err)
	}
	if err := outbox.Acknowledge([]*nodev1.AllocationLifecycleObservation{nonmatching}); err != nil {
		t.Fatalf("Acknowledge(nonmatching) error = %v", err)
	}

	replayed, err := outbox.Replay()
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].GetExitCode() != 42 {
		t.Fatalf("replayed observations = %#v, want only newer exit", replayed)
	}
}

func TestAllocationLifecycleOutboxRejectsNonterminalObservation(t *testing.T) {
	outbox := NewAllocationLifecycleOutbox(storetest.NewMockStore())
	_, err := outbox.Persist(&nodev1.AllocationLifecycleObservation{
		AllocationID: "alloc-1",
		State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		ObservedAt:   timestamppb.Now(),
	})
	if err == nil {
		t.Fatal("Persist(nonterminal) error = nil, want rejection")
	}
}

func TestAllocationLifecycleOutboxKeepsFirstTerminalProofForAllocation(t *testing.T) {
	outbox := NewAllocationLifecycleOutbox(storetest.NewMockStore())
	first := terminalStatusObservation("alloc-1", 17, time.Unix(100, 1).UTC())
	conflict := terminalStatusObservation("alloc-1", 42, time.Unix(100, 2).UTC())
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

func terminalStatusObservation(allocationID string, exitCode int32, observedAt time.Time) *nodev1.AllocationLifecycleObservation {
	return &nodev1.AllocationLifecycleObservation{
		AllocationID: allocationID,
		State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
		ExitCode:     &exitCode,
		ObservedAt:   timestamppb.New(observedAt),
	}
}
