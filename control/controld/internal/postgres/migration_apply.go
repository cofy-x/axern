package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const migrationAdvisoryLockID int64 = 0x415845524e4d4947 // AXERNMIG

func checkMigrations(ctx context.Context, db *DB) error {
	if db == nil || db.pool == nil {
		return fmt.Errorf("postgres db is nil")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := loadAppliedMigrations(ctx, db.pool)
	if err != nil {
		return fmt.Errorf("check postgres schema_migrations: %w", err)
	}
	known := make(map[int64]Migration, len(migrations))
	for _, migration := range migrations {
		known[migration.Version] = migration
		checksum, exists := applied[migration.Version]
		if !exists {
			return fmt.Errorf("postgres schema migration %06d %s is not applied; run controld-migrate up", migration.Version, migration.Name)
		}
		if checksum != migration.Checksum {
			return fmt.Errorf("postgres migration %06d checksum mismatch", migration.Version)
		}
	}
	for version := range applied {
		if _, exists := known[version]; !exists {
			return fmt.Errorf("postgres schema migration %06d is ahead of this binary", version)
		}
	}
	return nil
}

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
		return MigrationResult{}, fmt.Errorf("begin postgres migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationAdvisoryLockID); err != nil {
		return MigrationResult{}, fmt.Errorf("acquire postgres migration advisory lock: %w", err)
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
				return MigrationResult{}, fmt.Errorf("postgres migration %06d checksum mismatch", migration.Version)
			}
			result.Skipped = append(result.Skipped, migration)
			continue
		}
		if _, err := tx.Exec(ctx, migration.SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
			return MigrationResult{}, fmt.Errorf("apply postgres migration %06d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_migrations(version, name, checksum)
			VALUES ($1, $2, $3)
		`, migration.Version, migration.Name, migration.Checksum); err != nil {
			return MigrationResult{}, fmt.Errorf("record postgres migration %06d: %w", migration.Version, err)
		}
		result.Applied = append(result.Applied, migration)
	}
	if err := tx.Commit(ctx); err != nil {
		return MigrationResult{}, fmt.Errorf("commit postgres migration transaction: %w", err)
	}
	return result, nil
}

func ensureMigrationTable(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure postgres schema_migrations table: %w", err)
	}
	return nil
}
