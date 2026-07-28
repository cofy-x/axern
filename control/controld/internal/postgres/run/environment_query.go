package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error) {
	row := s.db.Pool().QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1`, strings.TrimSpace(id))
	env, err := scanEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "environment %q not found", id)
	}
	return env, err
}

func (s *Store) ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, error) {
	rows, err := s.db.Pool().Query(ctx, environmentSelectSQL()+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query environments: %w", err)
	}
	defer rows.Close()
	out := make([]*environmentv1.Environment, 0)
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		if environmentkernel.MatchFilter(env, filter) {
			out = append(out, env)
		}
	}
	return out, rows.Err()
}
