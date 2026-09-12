package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type retiredVolumeQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRetiredVolumeData refuses to attach the reduced control plane to legacy
// persistent-volume data. It is read-only: neither migration nor startup owns
// export, abandonment, or physical deletion of retired claims.
func (db *DB) CheckRetiredVolumeData(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return fmt.Errorf("postgres db is nil")
	}
	return checkRetiredVolumeData(ctx, db.pool)
}

func checkRetiredVolumeData(ctx context.Context, query retiredVolumeQuery) error {
	for _, table := range []struct {
		name string
		rows string
	}{
		{"storage_volume_claims", "SELECT EXISTS (SELECT 1 FROM storage_volume_claims)"},
		{"storage_volume_bindings", "SELECT EXISTS (SELECT 1 FROM storage_volume_bindings)"},
	} {
		var exists bool
		if err := query.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect retired volume table %s: %w", table.name, err)
		}
		if !exists {
			continue
		}
		var hasRows bool
		if err := query.QueryRow(ctx, table.rows).Scan(&hasRows); err != nil {
			return fmt.Errorf("inspect retired volume data in %s: %w", table.name, err)
		}
		if hasRows {
			return fmt.Errorf("retired persistent-volume data exists in %s; use the archived Axern version to inventory and export it, then start this release with new clean control and node state; no data has been changed", table.name)
		}
	}
	return nil
}
