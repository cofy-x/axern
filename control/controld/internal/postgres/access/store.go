package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct{ db *postgres.DB }

func NewStore(db *postgres.DB) *Store { return &Store{db: db} }

func (s *Store) ResolveActor(ctx context.Context, fingerprint [32]byte, now time.Time) (accesskernel.Actor, error) {
	var actor accesskernel.Actor
	row := s.db.Pool().QueryRow(ctx, `
		SELECT p.principal_id, p.name, p.display_name, p.kind, p.status, p.version, p.created_at, p.updated_at,
		       c.credential_id, c.certificate_not_after, c.label, c.created_at
		FROM principal_credentials c
		JOIN principals p ON p.principal_id = c.principal_id
		WHERE c.fingerprint = $1 AND c.revoked_at IS NULL AND c.certificate_not_after > $2
	`, fingerprint[:], now.UTC())
	if err := row.Scan(
		&actor.Principal.ID, &actor.Principal.Name, &actor.Principal.DisplayName, &actor.Principal.Kind,
		&actor.Principal.Status, &actor.Principal.Version, &actor.Principal.CreatedAt, &actor.Principal.UpdatedAt,
		&actor.Credential.ID, &actor.Credential.CertificateNotAfter, &actor.Credential.Label, &actor.Credential.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return accesskernel.Actor{}, accesskernel.ErrUnauthenticated
		}
		return accesskernel.Actor{}, fmt.Errorf("resolve principal credential: %w", err)
	}
	if actor.Principal.Status != accesskernel.PrincipalStatusActive {
		return accesskernel.Actor{}, accesskernel.ErrUnauthenticated
	}
	actor.Credential.PrincipalID = actor.Principal.ID
	actor.Credential.Fingerprint = fingerprint
	bindings, err := s.listActiveBindings(ctx, actor.Principal.ID)
	if err != nil {
		return accesskernel.Actor{}, err
	}
	actor.Bindings = bindings
	return actor, nil
}

func (s *Store) listActiveBindings(ctx context.Context, principalID string) ([]accesskernel.Binding, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT binding_id, principal_id, scope_type, COALESCE(namespace, ''), role,
		       COALESCE(created_by_principal_id, ''), created_at
		FROM role_bindings
		WHERE principal_id = $1 AND revoked_at IS NULL
		ORDER BY created_at, binding_id
	`, principalID)
	if err != nil {
		return nil, fmt.Errorf("query active role bindings: %w", err)
	}
	defer rows.Close()
	out := make([]accesskernel.Binding, 0)
	for rows.Next() {
		var binding accesskernel.Binding
		if err := rows.Scan(&binding.ID, &binding.PrincipalID, &binding.Scope, &binding.Namespace, &binding.Role, &binding.CreatedByPrincipalID, &binding.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (s *Store) HasActivePlatformAdmin(ctx context.Context, now time.Time) (bool, error) {
	var exists bool
	err := s.db.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM principals p
			JOIN role_bindings b ON b.principal_id = p.principal_id
			JOIN principal_credentials c ON c.principal_id = p.principal_id
			WHERE p.status = 'active' AND b.role = 'platform_admin' AND b.revoked_at IS NULL
			  AND c.revoked_at IS NULL AND c.certificate_not_after > $1
		)
	`, now.UTC()).Scan(&exists)
	return exists, err
}

func (s *Store) CreatePrincipal(ctx context.Context, actorID, name, displayName string, kind accesskernel.PrincipalKind, now time.Time) (accesskernel.Principal, error) {
	principal := accesskernel.Principal{ID: "prn-" + uuid.NewString(), Name: strings.TrimSpace(name), DisplayName: strings.TrimSpace(displayName), Kind: kind, Status: accesskernel.PrincipalStatusActive, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if err := accesskernel.ValidatePrincipalName(principal.Name); err != nil {
		return accesskernel.Principal{}, err
	}
	if principal.DisplayName == "" {
		return accesskernel.Principal{}, fmt.Errorf("%w: principal display name is required", accesskernel.ErrInvalidArgument)
	}
	if kind != accesskernel.PrincipalKindHuman && kind != accesskernel.PrincipalKindService {
		return accesskernel.Principal{}, fmt.Errorf("%w: valid principal kind is required", accesskernel.ErrInvalidArgument)
	}
	err := s.withAudit(ctx, actorID, "principal.create", "principal", principal.ID, now, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO principals (principal_id, name, display_name, kind, status, version, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,1,$6,$6)`, principal.ID, principal.Name, principal.DisplayName, principal.Kind, principal.Status, principal.CreatedAt)
		return err
	})
	if uniqueViolation(err) {
		return accesskernel.Principal{}, accesskernel.ErrAlreadyExists
	}
	return principal, err
}

func (s *Store) ListPrincipals(ctx context.Context) ([]accesskernel.Principal, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT principal_id, name, display_name, kind, status, version, created_at, updated_at FROM principals ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accesskernel.Principal, 0)
	for rows.Next() {
		var p accesskernel.Principal
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.Kind, &p.Status, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DisablePrincipal(ctx context.Context, actorID, principalID string, now time.Time) (accesskernel.Principal, error) {
	err := s.withAudit(ctx, actorID, "principal.disable", "principal", principalID, now, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE principals SET status='disabled', version=version+1, updated_at=$2 WHERE principal_id=$1 AND status='active'`, principalID, now.UTC())
		if err == nil && tag.RowsAffected() == 0 {
			return accesskernel.ErrNotFound
		}
		if err != nil {
			return err
		}
		return ensureActivePlatformAdminTx(ctx, tx, now)
	})
	if err != nil {
		return accesskernel.Principal{}, err
	}
	return s.getPrincipal(ctx, principalID)
}

func (s *Store) getPrincipal(ctx context.Context, id string) (accesskernel.Principal, error) {
	var p accesskernel.Principal
	err := s.db.Pool().QueryRow(ctx, `SELECT principal_id,name,display_name,kind,status,version,created_at,updated_at FROM principals WHERE principal_id=$1`, id).Scan(&p.ID, &p.Name, &p.DisplayName, &p.Kind, &p.Status, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = accesskernel.ErrNotFound
	}
	return p, err
}

func (s *Store) AddCredential(ctx context.Context, actorID, principalID, label string, fingerprint [32]byte, notAfter, now time.Time) (accesskernel.Credential, error) {
	credential := accesskernel.Credential{ID: "cred-" + uuid.NewString(), PrincipalID: principalID, Fingerprint: fingerprint, CertificateNotAfter: notAfter.UTC(), Label: strings.TrimSpace(label), CreatedAt: now.UTC()}
	if credential.Label == "" {
		return accesskernel.Credential{}, fmt.Errorf("%w: credential label is required", accesskernel.ErrInvalidArgument)
	}
	if !credential.CertificateNotAfter.After(now) {
		return accesskernel.Credential{}, fmt.Errorf("%w: certificate is expired", accesskernel.ErrInvalidArgument)
	}
	err := s.withAudit(ctx, actorID, "credential.add", "credential", credential.ID, now, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT status='active' FROM principals WHERE principal_id=$1 FOR UPDATE`, principalID).Scan(&active); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return accesskernel.ErrNotFound
			}
			return err
		}
		if !active {
			return fmt.Errorf("%w: principal is disabled", accesskernel.ErrFailedPrecondition)
		}
		_, err := tx.Exec(ctx, `INSERT INTO principal_credentials (credential_id,principal_id,kind,fingerprint,certificate_not_after,label,created_at) VALUES ($1,$2,'x509_sha256',$3,$4,$5,$6)`, credential.ID, principalID, fingerprint[:], credential.CertificateNotAfter, credential.Label, credential.CreatedAt)
		return err
	})
	if uniqueViolation(err) {
		return accesskernel.Credential{}, accesskernel.ErrAlreadyExists
	}
	return credential, err
}

func (s *Store) ListCredentials(ctx context.Context, principalID string) ([]accesskernel.Credential, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT credential_id,principal_id,fingerprint,certificate_not_after,label,created_at,revoked_at FROM principal_credentials WHERE principal_id=$1 ORDER BY created_at DESC`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accesskernel.Credential, 0)
	for rows.Next() {
		var c accesskernel.Credential
		var fp []byte
		if err := rows.Scan(&c.ID, &c.PrincipalID, &fp, &c.CertificateNotAfter, &c.Label, &c.CreatedAt, &c.RevokedAt); err != nil {
			return nil, err
		}
		copy(c.Fingerprint[:], fp)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) RevokeCredential(ctx context.Context, actorID, credentialID string, now time.Time) (accesskernel.Credential, error) {
	err := s.withAudit(ctx, actorID, "credential.revoke", "credential", credentialID, now, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE principal_credentials SET revoked_at=$2 WHERE credential_id=$1 AND revoked_at IS NULL`, credentialID, now.UTC())
		if err == nil && tag.RowsAffected() == 0 {
			return accesskernel.ErrNotFound
		}
		if err != nil {
			return err
		}
		return ensureActivePlatformAdminTx(ctx, tx, now)
	})
	if err != nil {
		return accesskernel.Credential{}, err
	}
	var c accesskernel.Credential
	var fp []byte
	err = s.db.Pool().QueryRow(ctx, `SELECT credential_id,principal_id,fingerprint,certificate_not_after,label,created_at,revoked_at FROM principal_credentials WHERE credential_id=$1`, credentialID).Scan(&c.ID, &c.PrincipalID, &fp, &c.CertificateNotAfter, &c.Label, &c.CreatedAt, &c.RevokedAt)
	copy(c.Fingerprint[:], fp)
	return c, err
}

func (s *Store) GrantBinding(ctx context.Context, actorID, principalID string, scope accesskernel.ScopeType, namespace string, role accesskernel.Role, now time.Time) (accesskernel.Binding, error) {
	b := accesskernel.Binding{ID: "rb-" + uuid.NewString(), PrincipalID: principalID, Scope: scope, Namespace: strings.TrimSpace(namespace), Role: role, CreatedByPrincipalID: actorID, CreatedAt: now.UTC()}
	if scope == accesskernel.ScopePlatform {
		b.Namespace = ""
		if role != accesskernel.RolePlatformAdmin {
			return accesskernel.Binding{}, fmt.Errorf("%w: invalid platform role", accesskernel.ErrInvalidArgument)
		}
	} else if scope == accesskernel.ScopeNamespace {
		if b.Namespace == "" || role == accesskernel.RolePlatformAdmin || !accesskernel.IsPublicRole(role) {
			return accesskernel.Binding{}, fmt.Errorf("%w: invalid namespace role binding", accesskernel.ErrInvalidArgument)
		}
	} else {
		return accesskernel.Binding{}, fmt.Errorf("%w: valid scope type is required", accesskernel.ErrInvalidArgument)
	}
	err := s.withAudit(ctx, actorID, "role_binding.grant", "role_binding", b.ID, now, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT status='active' FROM principals WHERE principal_id=$1 FOR UPDATE`, principalID).Scan(&active); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return accesskernel.ErrNotFound
			}
			return err
		}
		if !active {
			return fmt.Errorf("%w: principal is disabled", accesskernel.ErrFailedPrecondition)
		}
		if b.Scope == accesskernel.ScopeNamespace {
			var namespace string
			if err := tx.QueryRow(ctx, `SELECT namespace FROM namespaces WHERE namespace=$1 FOR KEY SHARE`, b.Namespace).Scan(&namespace); errors.Is(err, pgx.ErrNoRows) {
				return accesskernel.ErrNotFound
			} else if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_bindings(binding_id,principal_id,scope_type,namespace,role,created_by_principal_id,created_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7)`, b.ID, b.PrincipalID, b.Scope, b.Namespace, b.Role, b.CreatedByPrincipalID, b.CreatedAt)
		return err
	})
	if uniqueViolation(err) {
		return accesskernel.Binding{}, accesskernel.ErrAlreadyExists
	}
	return b, err
}

func (s *Store) ListBindings(ctx context.Context, principalID, namespace string, includeRevoked bool) ([]accesskernel.Binding, error) {
	query := `SELECT binding_id,principal_id,scope_type,COALESCE(namespace,''),role,COALESCE(created_by_principal_id,''),created_at,COALESCE(revoked_by_principal_id,''),revoked_at FROM role_bindings WHERE ($1='' OR principal_id=$1) AND ($2='' OR namespace=$2)`
	if !includeRevoked {
		query += ` AND revoked_at IS NULL`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.Pool().Query(ctx, query, principalID, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accesskernel.Binding, 0)
	for rows.Next() {
		var b accesskernel.Binding
		if err := rows.Scan(&b.ID, &b.PrincipalID, &b.Scope, &b.Namespace, &b.Role, &b.CreatedByPrincipalID, &b.CreatedAt, &b.RevokedByPrincipalID, &b.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBinding(ctx context.Context, bindingID string) (accesskernel.Binding, error) {
	var binding accesskernel.Binding
	err := s.db.Pool().QueryRow(ctx, `
		SELECT binding_id,principal_id,scope_type,COALESCE(namespace,''),role,
		       COALESCE(created_by_principal_id,''),created_at,
		       COALESCE(revoked_by_principal_id,''),revoked_at
		FROM role_bindings WHERE binding_id=$1
	`, bindingID).Scan(
		&binding.ID, &binding.PrincipalID, &binding.Scope, &binding.Namespace, &binding.Role,
		&binding.CreatedByPrincipalID, &binding.CreatedAt, &binding.RevokedByPrincipalID, &binding.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return accesskernel.Binding{}, accesskernel.ErrNotFound
	}
	return binding, err
}

func (s *Store) RevokeBinding(ctx context.Context, actorID, bindingID string, now time.Time) (accesskernel.Binding, error) {
	err := s.withAudit(ctx, actorID, "role_binding.revoke", "role_binding", bindingID, now, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE role_bindings SET revoked_by_principal_id=$2,revoked_at=$3 WHERE binding_id=$1 AND revoked_at IS NULL`, bindingID, actorID, now.UTC())
		if err == nil && tag.RowsAffected() == 0 {
			return accesskernel.ErrNotFound
		}
		if err != nil {
			return err
		}
		return ensureActivePlatformAdminTx(ctx, tx, now)
	})
	if err != nil {
		return accesskernel.Binding{}, err
	}
	return s.GetBinding(ctx, bindingID)
}

func (s *Store) withAudit(ctx context.Context, actorID, operation, targetType, targetID string, now time.Time, mutate func(pgx.Tx) error) error {
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize policy mutations with bootstrap so concurrent credential, Principal,
	// or binding revocations cannot each observe another active platform admin and
	// collectively remove the final administrator.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return err
	}
	if err := mutate(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO admin_audit_events(event_id,operation,target_type,target_id,operator_reason,actor_principal_id,created_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7)`, `admaudit-`+uuid.NewString(), operation, targetType, targetID, "access policy change", actorID, now.UTC()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE control_revisions SET revision=revision+1 WHERE name='access'`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ensureActivePlatformAdminTx(ctx context.Context, tx pgx.Tx, now time.Time) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM principals p
			JOIN role_bindings b ON b.principal_id=p.principal_id
			JOIN principal_credentials c ON c.principal_id=p.principal_id
			WHERE p.status='active' AND b.role='platform_admin' AND b.revoked_at IS NULL
			  AND c.revoked_at IS NULL AND c.certificate_not_after > $1
		)
	`, now.UTC()).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: access policy change would remove the last active platform administrator", accesskernel.ErrFailedPrecondition)
	}
	return nil
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
