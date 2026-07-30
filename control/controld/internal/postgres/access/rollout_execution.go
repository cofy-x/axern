package access

import (
	"context"
	"strings"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	"github.com/jackc/pgx/v5"
)

// ValidateRolloutExecutionLease validates the existing durable work lease as a
// short-lived delegation for public data-plane operations in its rollout's
// namespace. No plaintext lease is persisted.
func (s *Store) ValidateRolloutExecutionLease(ctx context.Context, token, namespace string, now time.Time) error {
	token = strings.TrimSpace(token)
	namespace = strings.TrimSpace(namespace)
	if token == "" || namespace == "" {
		return accesskernel.ErrPermissionDenied
	}
	var valid bool
	err := s.db.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM rollout_work_items w
			JOIN rollouts r ON r.rollout_id = w.rollout_id
			WHERE w.lease_token_hash = $1
			  AND w.status = 'LEASED'
			  AND w.lease_expires_at > $2
			  AND w.cancel_requested = FALSE
			  AND r.namespace = $3
		)
	`, rolloutkernel.HashToken(token), now.UTC(), namespace).Scan(&valid)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if !valid {
		return accesskernel.ErrPermissionDenied
	}
	return nil
}
