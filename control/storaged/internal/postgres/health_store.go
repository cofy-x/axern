package postgres

import (
	"context"
	"fmt"
	"time"

	kernel "github.com/cofy-x/axern/control/storaged/internal/kernel/storage"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"github.com/jackc/pgx/v5"
)

type volumeHealthQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) GetVolumeBindingHealth(ctx context.Context, releasingStuckAfter time.Duration) (*privatestoragev1.VolumeBindingHealth, error) {
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	bindingCounts, err := volumeStatusCountMap(ctx, tx, `
		SELECT status, count(*) FROM storage_volume_bindings
		GROUP BY status
		ORDER BY status
	`)
	if err != nil {
		return nil, err
	}
	claimCounts, err := volumeStatusCountMap(ctx, tx, `
		SELECT status, count(*) FROM storage_volume_claims
		GROUP BY status
		ORDER BY status
	`)
	if err != nil {
		return nil, err
	}
	stuckReleasingBindings, err := stuckReleasingBindingCount(ctx, tx, releasingStuckAfter)
	if err != nil {
		return nil, err
	}
	consistency, err := volumeHealthConsistency(ctx, tx)
	if err != nil {
		return nil, err
	}
	health := kernel.BuildVolumeBindingHealth(bindingCounts, claimCounts, stuckReleasingBindings, consistency)
	health.StuckDeletingClaims, err = stuckDeletingClaimCount(ctx, tx, releasingStuckAfter)
	if err != nil {
		return nil, err
	}
	return health, tx.Commit(ctx)
}

func stuckDeletingClaimCount(ctx context.Context, q volumeHealthQuerier, stuckAfter time.Duration) (int64, error) {
	if stuckAfter <= 0 {
		return 0, nil
	}
	var count int64
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM storage_volume_claims
		WHERE status = $1 AND updated_at <= $2
	`, storagev1.VolumeStatus_VOLUME_STATUS_DELETING.String(), time.Now().UTC().Add(-stuckAfter)).Scan(&count)
	return count, err
}

func volumeHealthConsistency(ctx context.Context, q volumeHealthQuerier) (kernel.HealthConsistencyCounts, error) {
	claims, err := volumeHealthClaims(ctx, q)
	if err != nil {
		return kernel.HealthConsistencyCounts{}, err
	}
	bindings, err := volumeHealthBindings(ctx, q)
	if err != nil {
		return kernel.HealthConsistencyCounts{}, err
	}
	return kernel.EvaluateHealthConsistency(claims, bindings), nil
}

func volumeHealthClaims(ctx context.Context, q volumeHealthQuerier) ([]kernel.ClaimHealthState, error) {
	rows, err := q.Query(ctx, `
		SELECT claim_id, status FROM storage_volume_claims
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []kernel.ClaimHealthState
	for rows.Next() {
		var claimID string
		var status string
		if err := rows.Scan(&claimID, &status); err != nil {
			return nil, err
		}
		statusValue, ok := storagev1.VolumeStatus_value[status]
		if !ok {
			return nil, fmt.Errorf("unknown volume status %q", status)
		}
		out = append(out, kernel.ClaimHealthState{ClaimID: claimID, Status: storagev1.VolumeStatus(statusValue)})
	}
	return out, rows.Err()
}

func volumeHealthBindings(ctx context.Context, q volumeHealthQuerier) ([]*privatestoragev1.VolumeBinding, error) {
	rows, err := q.Query(ctx, `
		SELECT payload FROM storage_volume_bindings
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*privatestoragev1.VolumeBinding
	for rows.Next() {
		binding, err := scanVolumeBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func stuckReleasingBindingCount(ctx context.Context, q volumeHealthQuerier, releasingStuckAfter time.Duration) (int64, error) {
	if releasingStuckAfter <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-releasingStuckAfter)
	var count int64
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM storage_volume_bindings
		WHERE status = $1 AND updated_at <= $2
	`, storagev1.VolumeStatus_VOLUME_STATUS_RELEASING.String(), cutoff).Scan(&count)
	return count, err
}

func volumeStatusCountMap(ctx context.Context, q volumeHealthQuerier, query string) (map[storagev1.VolumeStatus]int64, error) {
	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[storagev1.VolumeStatus]int64{}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		statusValue, ok := storagev1.VolumeStatus_value[status]
		if !ok {
			return nil, fmt.Errorf("unknown volume status %q", status)
		}
		out[storagev1.VolumeStatus(statusValue)] = count
	}
	return out, rows.Err()
}
