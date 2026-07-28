package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) GetRun(ctx context.Context, id string) (*runv1.Run, error) {
	run, err := scanRun(s.db.Pool().QueryRow(ctx, runSelectSQL()+` WHERE run_id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "run %q not found", id)
	}
	return run, err
}

func (s *Store) ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, error) {
	rows, err := s.db.Pool().Query(ctx, runSelectSQL()+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()
	out := make([]*runv1.Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		if runkernel.MatchFilter(run, filter) {
			out = append(out, run)
		}
	}
	return out, rows.Err()
}
