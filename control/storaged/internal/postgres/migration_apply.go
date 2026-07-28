package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const migrationAdvisoryLockID int64 = 0x415853544f524147 // AXSTORAG

func applyMigrations(ctx context.Context, db *DB) (MigrationResult, error) {
	if db == nil || db.pool == nil {
		return MigrationResult{}, fmt.Errorf("postgres db is nil")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return MigrationResult{}, err
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("begin storaged postgres migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationAdvisoryLockID); err != nil {
		return MigrationResult{}, fmt.Errorf("acquire storaged postgres migration advisory lock: %w", err)
	}
	if err := ensureMigrationTable(ctx, tx); err != nil {
		return MigrationResult{}, err
	}
	applied, err := loadAppliedMigrations(ctx, tx)
	if err != nil {
		return MigrationResult{}, err
	}
	result := MigrationResult{}
	for _, migration := range migrations {
		checksum, exists := applied[migration.Version]
		if exists {
			if checksum != migration.Checksum {
				return MigrationResult{}, fmt.Errorf("storaged postgres migration %06d checksum mismatch", migration.Version)
			}
			result.Skipped = append(result.Skipped, migration)
			continue
		}
		for _, stmt := range splitSQLStatements(migration.SQL) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return MigrationResult{}, fmt.Errorf("apply storaged postgres migration %06d %s: %w", migration.Version, migration.Name, err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO storaged_schema_migrations(version, name, checksum)
			VALUES ($1, $2, $3)
		`, migration.Version, migration.Name, migration.Checksum); err != nil {
			return MigrationResult{}, fmt.Errorf("record storaged postgres migration %06d: %w", migration.Version, err)
		}
		result.Applied = append(result.Applied, migration)
	}
	if err := tx.Commit(ctx); err != nil {
		return MigrationResult{}, fmt.Errorf("commit storaged postgres migration transaction: %w", err)
	}
	return result, nil
}

func ensureMigrationTable(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS storaged_schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure storaged postgres migration table: %w", err)
	}
	return nil
}
