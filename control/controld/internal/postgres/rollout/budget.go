package pgrollout

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ExpireBudgets(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT rollout_id FROM rollouts WHERE deadline IS NOT NULL AND deadline<=$1 AND status IN('ROLLOUT_STATUS_ACCEPTED','ROLLOUT_STATUS_PLANNING','ROLLOUT_STATUS_READY','ROLLOUT_STATUS_QUEUED','ROLLOUT_STATUS_RUNNING') ORDER BY deadline,rollout_id LIMIT 100`, now.UTC())
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	expired := 0
	for _, id := range ids {
		tx, err := s.db.Pool().Begin(ctx)
		if err != nil {
			return expired, err
		}
		ok, err := expireBudgetTx(ctx, tx, id, now)
		if err != nil {
			_ = tx.Rollback(ctx)
			return expired, err
		}
		if err := tx.Commit(ctx); err != nil {
			return expired, err
		}
		if ok {
			expired++
		}
	}
	return expired, nil
}
func expireBudgetTx(ctx context.Context, tx pgx.Tx, id string, now time.Time) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT work_id,status FROM rollout_work_items WHERE rollout_id=$1 AND status IN('HELD','PENDING','LEASED') ORDER BY work_id FOR UPDATE`, id)
	if err != nil {
		return false, err
	}
	var pendingWorkIDs []string
	for rows.Next() {
		var workID, workStatus string
		if err := rows.Scan(&workID, &workStatus); err != nil {
			rows.Close()
			return false, err
		}
		if workStatus == workStatusPending || workStatus == "HELD" {
			pendingWorkIDs = append(pendingWorkIDs, workID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	var eligible bool
	if err := tx.QueryRow(ctx, `SELECT deadline<=$2 AND status IN('ROLLOUT_STATUS_ACCEPTED','ROLLOUT_STATUS_PLANNING','ROLLOUT_STATUS_READY','ROLLOUT_STATUS_QUEUED','ROLLOUT_STATUS_RUNNING') FROM rollouts WHERE rollout_id=$1 FOR UPDATE`, id, now.UTC()).Scan(&eligible); err != nil {
		return false, err
	}
	if !eligible {
		return false, nil
	}
	if err := releaseUsageReservationsForWorkIDs(ctx, tx, pendingWorkIDs, now); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_work_items SET status=CASE WHEN status IN('HELD','PENDING') THEN 'CANCELLED' ELSE status END,cancel_requested=CASE WHEN status='LEASED' THEN TRUE ELSE cancel_requested END,updated_at=$2 WHERE rollout_id=$1 AND status IN('HELD','PENDING','LEASED')`, id, now.UTC()); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollout_episodes SET status='EPISODE_STATUS_FAILED',failure_class='FAILURE_CLASS_BUDGET',message='wall-time budget exhausted',completed_at=$2 WHERE rollout_id=$1 AND status NOT IN('EPISODE_STATUS_COMPLETED','EPISODE_STATUS_FAILED','EPISODE_STATUS_CANCELLED')`, id, now.UTC()); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE rollouts SET status='ROLLOUT_STATUS_FAILED',failure_class='FAILURE_CLASS_BUDGET',message='wall-time budget exhausted',completed_at=$2,version=version+1 WHERE rollout_id=$1`, id, now.UTC()); err != nil {
		return false, err
	}
	if err := insertEventTx(ctx, tx, id, "", "rollout.budget_exhausted", "budget", "wall-time budget exhausted", map[string]string{"budget": "max_wall_time"}, now); err != nil {
		return false, fmt.Errorf("record budget event: %w", err)
	}
	return true, nil
}
