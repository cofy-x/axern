package pgallocation

import (
	"context"
	"fmt"
	"strings"
	"time"

	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
)

type DeleteHistoryRequest struct {
	AllocationIDs []string
	Reason        string
	Now           time.Time
}

func DeleteHistoryTx(ctx context.Context, tx pgx.Tx, req DeleteHistoryRequest) (int64, error) {
	allocationIDs := compactAllocationIDs(req.AllocationIDs)
	if len(allocationIDs) == 0 {
		return 0, nil
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "delete allocation history"
	}
	now := req.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `DELETE FROM execution_leases WHERE allocation_id = ANY($1)`, allocationIDs); err != nil {
		return 0, fmt.Errorf("%s leases: %w", reason, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = ANY($1)`, allocationIDs); err != nil {
		return 0, fmt.Errorf("%s reconcile queue: %w", reason, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workload_reservations WHERE allocation_id = ANY($1)`, allocationIDs); err != nil {
		return 0, fmt.Errorf("%s reservations: %w", reason, err)
	}
	if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
		AllocationIDs: allocationIDs,
		Reason:        reason + " allocation deleted",
		ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_DELETED,
		Now:           now,
	}); err != nil {
		return 0, fmt.Errorf("%s tunnel sessions: %w", reason, err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM allocations WHERE allocation_id = ANY($1)`, allocationIDs)
	if err != nil {
		return 0, fmt.Errorf("%s allocations: %w", reason, err)
	}
	return tag.RowsAffected(), nil
}

func compactAllocationIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
