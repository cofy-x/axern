package pgrollout

import (
	"context"
	"time"
)

const (
	doctorTerminalRetention = 5 * time.Minute
	doctorOrphanRetention   = 24 * time.Hour
)

// ReconcileDoctorJobs bounds the lifetime of durable doctor jobs left behind
// when the requesting controld process exits before its request-scoped cleanup.
// Work items are removed by the doctor-job foreign key cascade.
func (s *Store) ReconcileDoctorJobs(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deleted int
	err = tx.QueryRow(ctx, `WITH expired AS (
		SELECT job_id FROM agent_profile_doctor_jobs
		WHERE (completed_at IS NOT NULL AND completed_at <= $1)
		   OR created_at <= $2
		ORDER BY created_at,job_id
		FOR UPDATE SKIP LOCKED
		LIMIT $3
	), removed AS (
		DELETE FROM agent_profile_doctor_jobs j USING expired e
		WHERE j.job_id=e.job_id
		RETURNING j.job_id
	)
	SELECT count(*) FROM removed`, now.UTC().Add(-doctorTerminalRetention), now.UTC().Add(-doctorOrphanRetention), limit).Scan(&deleted)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM secrets s
		WHERE s.visibility='INTERNAL' AND s.owner_type='AGENT_PROFILE'
		AND NOT EXISTS (SELECT 1 FROM agent_profiles p WHERE p.credential_secret_id=s.secret_id)
		AND NOT EXISTS (SELECT 1 FROM rollouts r WHERE r.frozen_credential_secret_id=s.secret_id)
		AND NOT EXISTS (SELECT 1 FROM agent_profile_doctor_jobs j WHERE j.frozen_credential_secret_id=s.secret_id)`); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}
