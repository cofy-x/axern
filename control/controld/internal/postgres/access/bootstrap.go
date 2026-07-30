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

func (s *Store) BootstrapPlatformAdmin(ctx context.Context, name, displayName, label string, fingerprint [32]byte, notAfter, now time.Time) error {
	if err := accesskernel.ValidatePrincipalName(name); err != nil {
		return err
	}
	if displayName == "" || label == "" {
		return errors.New("bootstrap display name and credential label are required")
	}
	if !notAfter.After(now) {
		return errors.New("bootstrap certificate is expired")
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
		return verifyBootstrapState(ctx, tx, name, displayName, label, fingerprint, notAfter)
	}
	principalID := "prn-" + uuid.NewString()
	credentialID := "cred-" + uuid.NewString()
	bindingID := "rb-" + uuid.NewString()
	now = now.UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO principals(principal_id,name,display_name,kind,status,version,created_at,updated_at) VALUES($1,$2,$3,'human','active',1,$4,$4)`, principalID, name, displayName, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO principal_credentials(credential_id,principal_id,kind,fingerprint,certificate_not_after,label,created_at) VALUES($1,$2,'x509_sha256',$3,$4,$5,$6)`, credentialID, principalID, fingerprint[:], notAfter.UTC(), label, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_bindings(binding_id,principal_id,scope_type,role,created_by_principal_id,created_at) VALUES($1,$2,'platform','platform_admin',$2,$3)`, bindingID, principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO admin_audit_events(event_id,operation,target_type,target_id,operator_reason,actor_principal_id,created_at) VALUES($1,'access.bootstrap','principal',$2,'initial platform administrator',$2,$3)`, `admaudit-`+uuid.NewString(), principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE control_revisions SET revision=revision+1 WHERE name='access'`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyBootstrapState(ctx context.Context, tx pgx.Tx, name, displayName, label string, fingerprint [32]byte, notAfter time.Time) error {
	var actualNotAfter time.Time
	err := tx.QueryRow(ctx, `
		SELECT c.certificate_not_after FROM principals p
		JOIN principal_credentials c ON c.principal_id=p.principal_id
		JOIN role_bindings b ON b.principal_id=p.principal_id
		WHERE p.name=$1 AND p.display_name=$3 AND p.kind='human' AND p.status='active'
		  AND c.fingerprint=$2 AND c.revoked_at IS NULL
		  AND c.label=$4
		  AND b.scope_type='platform' AND b.role='platform_admin' AND b.revoked_at IS NULL
	`, name, fingerprint[:], displayName, label).Scan(&actualNotAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("access bootstrap refused: authorization state already exists but does not match the requested administrator")
	}
	if err != nil {
		return fmt.Errorf("verify access bootstrap state: %w", err)
	}
	if !actualNotAfter.Equal(notAfter.UTC()) {
		return errors.New("access bootstrap refused: registered certificate metadata differs")
	}
	return tx.Commit(ctx)
}

func (s *Store) BootstrapRolloutExecutor(ctx context.Context, fingerprint [32]byte, notAfter, now time.Time) error {
	if !notAfter.After(now) {
		return errors.New("rollout worker certificate is expired")
	}
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLock); err != nil {
		return err
	}
	var principalID, status, kind string
	err = tx.QueryRow(ctx, `SELECT principal_id,status,kind FROM principals WHERE name='rollout-worker' FOR UPDATE`).Scan(&principalID, &status, &kind)
	if err == nil {
		if status != "active" || kind != "service" {
			return errors.New("access bootstrap refused: rollout-worker Principal has invalid state")
		}
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principal_credentials c JOIN role_bindings b ON b.principal_id=c.principal_id WHERE c.principal_id=$1 AND c.fingerprint=$2 AND c.certificate_not_after=$3 AND c.revoked_at IS NULL AND b.role='rollout_executor' AND b.revoked_at IS NULL)`, principalID, fingerprint[:], notAfter.UTC()).Scan(&found); err != nil {
			return err
		}
		if !found {
			return errors.New("access bootstrap refused: rollout-worker credential or role differs")
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	principalID = "prn-" + uuid.NewString()
	credentialID := "cred-" + uuid.NewString()
	bindingID := "rb-" + uuid.NewString()
	now = now.UTC()
	if _, err := tx.Exec(ctx, `INSERT INTO principals(principal_id,name,display_name,kind,status,version,created_at,updated_at) VALUES($1,'rollout-worker','Managed Rollout Worker','service','active',1,$2,$2)`, principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO principal_credentials(credential_id,principal_id,kind,fingerprint,certificate_not_after,label,created_at) VALUES($1,$2,'x509_sha256',$3,$4,'rollout-worker',$5)`, credentialID, principalID, fingerprint[:], notAfter.UTC(), now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_bindings(binding_id,principal_id,scope_type,role,created_by_principal_id,created_at) VALUES($1,$2,'platform','rollout_executor',$2,$3)`, bindingID, principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO admin_audit_events(event_id,operation,target_type,target_id,operator_reason,actor_principal_id,created_at) VALUES($1,'access.bootstrap','principal',$2,'managed rollout executor',$2,$3)`, `admaudit-`+uuid.NewString(), principalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE control_revisions SET revision=revision+1 WHERE name='access'`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
