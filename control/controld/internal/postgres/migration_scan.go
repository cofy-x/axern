package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type migrationQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadAppliedMigrations(ctx context.Context, q migrationQuerier) (map[int64]string, error) {
	rows, err := q.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query postgres schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan postgres schema_migrations: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres schema_migrations: %w", err)
	}
	return applied, nil
}
