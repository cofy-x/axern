package pgtunnel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func nextRevision(ctx context.Context, tx pgx.Tx) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE control_revisions
		SET revision = revision + 1
		WHERE name = 'tunnel_sessions'
		RETURNING revision
	`).Scan(&revision); err != nil {
		return 0, fmt.Errorf("advance tunnel revision: %w", err)
	}
	return revision, nil
}

type revisionQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type portQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func currentRevision(ctx context.Context, q revisionQuerier) (int64, error) {
	var revision int64
	err := q.QueryRow(ctx, `SELECT revision FROM control_revisions WHERE name = 'tunnel_sessions'`).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}
