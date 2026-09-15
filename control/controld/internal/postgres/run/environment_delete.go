package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) DeleteEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error) {
	var env *environmentv1.Environment
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		env, err = scanEnvironment(tx.QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1 FOR UPDATE`, strings.TrimSpace(id)))
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "environment %q not found", id)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM environments WHERE environment_id = $1`, strings.TrimSpace(id)); err != nil {
			return fmt.Errorf("delete environment: %w", err)
		}
		return nil
	})
	return env, err
}
