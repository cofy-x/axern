package service

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
)

func TestMemoryObservationSequenceReservesBeforeUseAndSkipsBlockAfterRestart(t *testing.T) {
	store := storetest.NewMockStore()
	first := &sandboxService{store: store}
	if err := first.initializeMemoryObservationSequence(); err != nil {
		t.Fatal(err)
	}
	revision, err := first.nextMemoryObservationRevision()
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("first revision = %d, want 1", revision)
	}
	var stored apipb.DurableSequence
	if err := store.LoadSnapshot(config.MemoryObservationSequenceBucket, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.GetReservedThrough() != memoryObservationSequenceBlock {
		t.Fatalf("reserved through = %d, want %d", stored.GetReservedThrough(), memoryObservationSequenceBlock)
	}

	restarted := &sandboxService{store: store}
	if err := restarted.initializeMemoryObservationSequence(); err != nil {
		t.Fatal(err)
	}
	revision, err = restarted.nextMemoryObservationRevision()
	if err != nil {
		t.Fatal(err)
	}
	if revision != memoryObservationSequenceBlock+1 {
		t.Fatalf("post-restart revision = %d, want %d", revision, memoryObservationSequenceBlock+1)
	}
}
