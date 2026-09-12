package access

import (
	"context"
	"errors"
	"fmt"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ResolveResourceNamespace(ctx context.Context, resourceType, resourceID string) (string, error) {
	queries := map[string]string{
		"environment": `SELECT namespace FROM environments WHERE environment_id=$1`,
		"run":         `SELECT namespace FROM runs WHERE run_id=$1`,
		"secret":      `SELECT namespace FROM secrets WHERE secret_id=$1`,
		"tunnel":      `SELECT namespace FROM tunnel_sessions WHERE session_id=$1`,
		"allocation": `
			SELECT namespace FROM workload_reservations
			WHERE allocation_id=$1 ORDER BY created_at DESC LIMIT 1`,
	}
	query, ok := queries[resourceType]
	if !ok {
		return "", fmt.Errorf("unsupported authorization resource type %q", resourceType)
	}
	var namespace string
	if err := s.db.Pool().QueryRow(ctx, query, resourceID).Scan(&namespace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", accesskernel.ErrNotFound
		}
		return "", fmt.Errorf("resolve %s namespace: %w", resourceType, err)
	}
	return namespace, nil
}
