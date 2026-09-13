package pgrun

import (
	"context"
	"time"

	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) RecordAllocationCapabilityConditions(ctx context.Context, allocationID string, conditions *capabilityv1.CapabilityConditionSet, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		return pgallocation.ReplaceCapabilityConditions(ctx, tx, allocationID, conditions, now)
	})
}
