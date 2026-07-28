package pgrun

import (
	"context"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	"github.com/jackc/pgx/v5"
)

func (s *Store) reserveCandidate(ctx context.Context, tx pgx.Tx, req pgreservation.ReserveCandidateRequest) (*nodekernel.Record, error) {
	return s.reservations.ReserveCandidate(ctx, tx, req)
}
