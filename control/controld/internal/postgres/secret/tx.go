package pgsecret

import (
	"context"
	"errors"
	"fmt"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/jackc/pgx/v5"
)

func withTx(ctx context.Context, db *postgres.DB, fn func(pgx.Tx) error) error {
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
