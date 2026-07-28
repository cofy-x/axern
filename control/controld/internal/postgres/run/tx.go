package pgrun

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/jackc/pgx/v5"
)

func (s *Store) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin run kernel tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		if postgres.ShouldCommitError(err) {
			if commit := tx.Commit(ctx); commit != nil {
				return fmt.Errorf("commit run kernel tx: %w", commit)
			}
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit run kernel tx: %w", err)
	}
	return nil
}
