package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthorizeTransaction re-reads and locks live authority until the authoritative
// write commits. The actor captured by the interceptor is identity, not a cache
// of permission that can survive credential or membership revocation.
func AuthorizeTransaction(ctx context.Context, tx pgx.Tx, action accesskernel.Action, namespace string, now time.Time) error {
	started := time.Now()
	saved, ok := accesskernel.ActorFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "authenticated Principal is required")
	}
	var actor accesskernel.Actor
	err := tx.QueryRow(ctx, `SELECT p.principal_id, p.status, c.credential_id, c.kind, c.expires_at
		FROM principal_credentials c JOIN principals p ON p.principal_id=c.principal_id
		WHERE c.fingerprint=$1 AND c.revoked_at IS NULL AND p.status='active'
		FOR SHARE OF p,c`, saved.Credential.Fingerprint[:]).Scan(&actor.Principal.ID, &actor.Principal.Status, &actor.Credential.ID, &actor.Credential.Kind, &actor.Credential.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return status.Error(codes.Unauthenticated, "client credential is not active")
	}
	if err != nil {
		return fmt.Errorf("lock transaction credential: %w", err)
	}
	if actor.Principal.ID != saved.Principal.ID || actor.Credential.Kind != accesskernel.CredentialX509 || !actor.Credential.ExpiresAt.After(now) {
		return status.Error(codes.Unauthenticated, "client credential is not active")
	}
	rows, err := tx.Query(ctx, `SELECT scope_type,COALESCE(namespace,''),role FROM role_bindings WHERE principal_id=$1 AND revoked_at IS NULL ORDER BY binding_id FOR SHARE`, actor.Principal.ID)
	if err != nil {
		return fmt.Errorf("lock transaction role bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var binding accesskernel.Binding
		if err := rows.Scan(&binding.Scope, &binding.Namespace, &binding.Role); err != nil {
			return err
		}
		actor.Bindings = append(actor.Bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !actor.Credential.ExpiresAt.After(now.Add(time.Since(started))) {
		return status.Error(codes.Unauthenticated, "client credential expired before admission")
	}
	if !accesskernel.Authorize(actor, action, namespace) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}
