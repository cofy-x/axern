package pgrun

import (
	"context"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	"github.com/jackc/pgx/v5"
)

func (s *Store) reserveCandidate(ctx context.Context, tx pgx.Tx, req pgreservation.ReserveCandidateRequest) (*placementkernel.AdmissionDecision, error) {
	return s.reservations.ReserveCandidate(ctx, tx, req)
}
