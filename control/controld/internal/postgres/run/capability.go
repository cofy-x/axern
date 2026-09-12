package pgrun

import (
	"context"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) RecordAllocationCapabilityAdmission(ctx context.Context, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := pgallocation.RecordCapabilityAdmission(ctx, tx, allocationID, admission, now); err != nil {
			return err
		}
		workspaceJSON := []byte("null")
		if admission.WorkspacePreparation != nil {
			var err error
			workspaceJSON, err = protojson.Marshal(admission.WorkspacePreparation)
			if err != nil {
				return fmt.Errorf("marshal workspace preparation: %w", err)
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE allocations
			SET workspace_preparation = $2::jsonb, updated_at = $3, version = version + 1
			WHERE allocation_id = $1 AND owner_type = $4 AND attempt = $5
		`, allocationID, workspaceJSON, now.UTC(), allocationOwnerRun, admission.Attempt)
		if err != nil {
			return fmt.Errorf("record run workspace preparation: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("record run workspace preparation: allocation %s attempt %d not found", allocationID, admission.Attempt)
		}
		return nil
	})
}
