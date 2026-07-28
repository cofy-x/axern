package pgrun

import (
	"context"
	"strings"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) currentAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runkernel.AllocationRecord, error) {
	var alloc runkernel.AllocationRecord
	if err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target, a.attempt
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&alloc.AllocationID, &alloc.NodeID, &alloc.NodeTarget, &alloc.Attempt); err != nil {
		return nil, err
	}
	return &alloc, nil
}

func (s *Store) runByAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*runv1.Run, error) {
	return scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE allocation_id = $1`, strings.TrimSpace(allocationID)))
}
