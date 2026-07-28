package reservation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func ReleaseAllocation(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) error {
	return ReleaseAllocations(ctx, tx, []string{allocationID}, now)
}

func ReleaseAllocations(ctx context.Context, tx pgx.Tx, allocationIDs []string, now time.Time) error {
	allocationIDs = compactAllocationIDs(allocationIDs)
	if len(allocationIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE workload_reservations
		SET released_at = COALESCE(released_at, $2)
		WHERE allocation_id = ANY($1::text[])
	`, allocationIDs, now.UTC()); err != nil {
		return fmt.Errorf("release reservations: %w", err)
	}
	return nil
}

func compactAllocationIDs(allocationIDs []string) []string {
	seen := make(map[string]struct{}, len(allocationIDs))
	out := make([]string, 0, len(allocationIDs))
	for _, allocationID := range allocationIDs {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			continue
		}
		if _, ok := seen[allocationID]; ok {
			continue
		}
		seen[allocationID] = struct{}{}
		out = append(out, allocationID)
	}
	return out
}
