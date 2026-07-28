package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) DeleteEnvironment(ctx context.Context, id string, now time.Time) (*environmentv1.Environment, error) {
	var env *environmentv1.Environment
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE environments
			SET status = $2, version = version + 1, updated_at = $3
			WHERE environment_id = $1
		`, strings.TrimSpace(id), environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED.String(), now.UTC()); err != nil {
			return fmt.Errorf("delete environment: %w", err)
		}
		var err error
		env, err = scanEnvironment(tx.QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1`, strings.TrimSpace(id)))
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "environment %q not found", id)
		}
		return err
	})
	return env, err
}
