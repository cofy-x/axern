package pgservice

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
)

func (s *PGStore) RecordCapabilityVerification(ctx context.Context, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	return pgallocation.RecordCapabilityVerification(ctx, s.db.Pool(), allocationID, admission, now)
}
