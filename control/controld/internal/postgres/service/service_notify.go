package pgservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const serviceChangeChannel = "axern_service_changes"

func notifyServiceChanged(ctx context.Context, tx pgx.Tx, serviceID string) error {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return fmt.Errorf("notify service change: service id is required")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, serviceChangeChannel, serviceID); err != nil {
		return fmt.Errorf("notify service change: %w", err)
	}
	return nil
}
