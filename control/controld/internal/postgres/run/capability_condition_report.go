package pgrun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
)

// BatchReportAllocationCapabilityConditions replaces allocation condition
// projections without touching lifecycle state. Node and attempt fencing is
// checked while each allocation row is locked in deterministic order.
func (s *Store) BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error {
	ordered := append([]*controlnodev1.AllocationCapabilityConditionReport(nil), reports...)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.TrimSpace(ordered[i].GetAllocationID()) < strings.TrimSpace(ordered[j].GetAllocationID())
	})
	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, report := range ordered {
			allocationID := strings.TrimSpace(report.GetAllocationID())
			var admittedNodeID string
			var attempt int64
			err := tx.QueryRow(ctx, `
				SELECT node_id, attempt FROM allocations
				WHERE allocation_id = $1
				FOR UPDATE
			`, allocationID).Scan(&admittedNodeID, &attempt)
			if errors.Is(err, pgx.ErrNoRows) {
				// The allocation may have reached terminal cleanup while a durable
				// node-side condition report was in flight. It is already fenced.
				continue
			}
			if err != nil {
				return fmt.Errorf("lock allocation %q capability conditions: %w", allocationID, err)
			}
			if admittedNodeID != strings.TrimSpace(nodeID) {
				continue
			}
			if attempt != report.GetAttempt() {
				continue
			}
			if err := pgallocation.ReplaceCapabilityConditions(ctx, tx, allocationID, report.GetConditionSet(), now); err != nil {
				return err
			}
		}
		return nil
	})
}
