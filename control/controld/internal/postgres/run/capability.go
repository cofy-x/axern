package pgrun

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/jackc/pgx/v5"
)

func (s *Store) RecordAllocationCapabilityAdmission(ctx context.Context, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := pgallocation.RecordCapabilityAdmission(ctx, tx, allocationID, admission, now); err != nil {
			return err
		}
		return nil
	})
}
