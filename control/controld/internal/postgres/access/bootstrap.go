package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const bootstrapAdvisoryLock int64 = 0x617865726e617574

func (s *Store) BootstrapPlatformAdmin(ctx context.Context, name, displayName, label string, credentials []accesskernel.CredentialMaterial, now time.Time) error {
	if err := accesskernel.ValidatePrincipalName(name); err != nil {
		return err
	}
	if displayName == "" || label == "" {
		return errors.New("bootstrap display name and credential label are required")
	}
	if len(credentials) == 0 || credentials[0].Kind != accesskernel.CredentialX509 {
		return errors.New("bootstrap requires an X.509 administrator credential")
	}
	for _, credential := range credentials {
		if !credential.ExpiresAt.After(now) {
			return errors.New("bootstrap credential is expired")
		}
		if credential.Kind != accesskernel.CredentialX509 && credential.Kind != accesskernel.CredentialSSH {
			return errors.New("unsupported bootstrap credential kind")
		}
	}
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return verifyBootstrapState(ctx, tx, name, displayName, label, credentials)
	}
	principalID := "prn-" + uuid.NewString()
	bindingID := "rb-" + uuid.NewString()
	now = now.UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO principals(principal_id,name,display_name,kind,status,created_at,updated_at) VALUES($1,$2,$3,'human','active',$4,$4)`, principalID, name, displayName, now); err != nil {
		return err
	}
	for _, credential := range credentials {
		if _, err := tx.Exec(ctx, `INSERT INTO principal_credentials(credential_id,principal_id,kind,fingerprint,expires_at,label,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, "cred-"+uuid.NewString(), principalID, credential.Kind, credential.Fingerprint[:], credential.ExpiresAt.UTC(), label, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_bindings(binding_id,principal_id,scope_type,role,created_by_principal_id,created_at) VALUES($1,$2,'platform','platform_admin',$2,$3)`, bindingID, principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO admin_audit_events(event_id,operation,target_type,target_id,operator_reason,actor_principal_id,created_at) VALUES($1,'access.bootstrap','principal',$2,'initial platform administrator',$2,$3)`, `admaudit-`+uuid.NewString(), principalID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyBootstrapState(ctx context.Context, tx pgx.Tx, name, displayName, label string, credentials []accesskernel.CredentialMaterial) error {
	for _, credential := range credentials {
		var actualNotAfter time.Time
		err := tx.QueryRow(ctx, `
		SELECT c.expires_at FROM principals p
		JOIN principal_credentials c ON c.principal_id=p.principal_id
		JOIN role_bindings b ON b.principal_id=p.principal_id
		WHERE p.name=$1 AND p.display_name=$3 AND p.kind='human' AND p.status='active'
		  AND c.fingerprint=$2 AND c.revoked_at IS NULL
		  AND c.label=$4 AND c.kind=$5
		  AND b.scope_type='platform' AND b.role='platform_admin' AND b.revoked_at IS NULL
	`, name, credential.Fingerprint[:], displayName, label, credential.Kind).Scan(&actualNotAfter)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("access bootstrap refused: authorization state already exists but does not match the requested administrator")
		}
		if err != nil {
			return fmt.Errorf("verify access bootstrap state: %w", err)
		}
		if !actualNotAfter.Equal(credential.ExpiresAt.UTC()) {
			return errors.New("access bootstrap refused: registered credential metadata differs")
		}
	}
	return tx.Commit(ctx)
}
