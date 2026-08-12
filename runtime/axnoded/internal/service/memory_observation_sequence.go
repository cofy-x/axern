package service

import (
	"fmt"
	"math"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

const memoryObservationSequenceBlock int64 = 1024

// initializeMemoryObservationSequence loads only the durable high watermark.
// Values at or below it are deliberately skipped after restart: gaps are safe,
// while reusing a revision could make a fresh observation permanently lose to
// an older control-plane row.
func (h *sandboxService) initializeMemoryObservationSequence() error {
	if h == nil || h.store == nil {
		return fmt.Errorf("initialize memory observation sequence: node state is unavailable")
	}
	h.memoryObservationMu.Lock()
	defer h.memoryObservationMu.Unlock()
	var stored apipb.DurableSequence
	if err := h.store.LoadSnapshot(config.MemoryObservationSequenceBucket, &stored); err != nil && !errord.IsNotFound(err) {
		return fmt.Errorf("load memory observation sequence: %w", err)
	}
	if stored.GetReservedThrough() < 0 || stored.GetReservedThrough() == math.MaxInt64 {
		return fmt.Errorf("memory observation sequence is invalid or exhausted")
	}
	h.memoryObservationReserved = stored.GetReservedThrough()
	h.memoryObservationNext = stored.GetReservedThrough() + 1
	return nil
}

func (h *sandboxService) nextMemoryObservationRevision() (int64, error) {
	if h == nil || h.store == nil {
		return 0, fmt.Errorf("allocate memory observation revision: node state is unavailable")
	}
	h.memoryObservationMu.Lock()
	defer h.memoryObservationMu.Unlock()
	if h.memoryObservationNext <= 0 {
		return 0, fmt.Errorf("allocate memory observation revision: sequence is not initialized")
	}
	if h.memoryObservationNext > h.memoryObservationReserved {
		if h.memoryObservationNext > math.MaxInt64-memoryObservationSequenceBlock+1 {
			return 0, fmt.Errorf("allocate memory observation revision: sequence exhausted")
		}
		reservedThrough := h.memoryObservationNext + memoryObservationSequenceBlock - 1
		if err := h.store.SaveSnapshot(config.MemoryObservationSequenceBucket, &apipb.DurableSequence{ReservedThrough: reservedThrough}); err != nil {
			return 0, fmt.Errorf("reserve memory observation revision block: %w", err)
		}
		h.memoryObservationReserved = reservedThrough
	}
	revision := h.memoryObservationNext
	h.memoryObservationNext++
	return revision, nil
}
