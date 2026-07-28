package postgres

import "context"

type Migration struct {
	Version  int64
	Name     string
	Checksum string
	SQL      string
}

type MigrationResult struct {
	Applied []Migration
	Skipped []Migration
}

func (r MigrationResult) AppliedCount() int {
	return len(r.Applied)
}

func (r MigrationResult) SkippedCount() int {
	return len(r.Skipped)
}

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.ApplyMigrations(ctx)
	return err
}

func (db *DB) ApplyMigrations(ctx context.Context) (MigrationResult, error) {
	return applyMigrations(ctx, db)
}
