package pgrun

import (
	"context"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	pgresourceadmission "github.com/cofy-x/axern/control/controld/internal/postgres/resourceadmission"
	"github.com/jackc/pgx/v5"
)

func (s *Store) admitCandidateResources(ctx context.Context, tx pgx.Tx, req pgresourceadmission.AdmitCandidateRequest) (*placementkernel.AdmissionDecision, error) {
	return s.resourceAdmission.AdmitCandidate(ctx, tx, req)
}
