package pgretention

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const advisoryLockID int64 = 0x415845524e52544e // AXERNRTN

func (s *PGStore) tryAdvisoryLock(ctx context.Context, tx pgx.Tx) (bool, error) {
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, advisoryLockID).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire retention advisory lock: %w", err)
	}
	return locked, nil
}

func (s *PGStore) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin retention tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit retention tx: %w", err)
	}
	return nil
}
