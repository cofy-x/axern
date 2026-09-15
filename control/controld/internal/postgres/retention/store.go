package pgretention

import (
	"context"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	retention "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/jackc/pgx/v5"
)

type PGStore struct {
	db *postgres.DB
}

func NewPGStore(db *postgres.DB) *PGStore {
	return &PGStore{db: db}
}

func (s *PGStore) Cleanup(ctx context.Context, cfg retention.Config, now time.Time) (retention.Result, error) {
	cfg = retention.NormalizeConfig(cfg)
	var result retention.Result
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		locked, lockErr := s.tryAdvisoryLock(ctx, tx)
		if lockErr != nil {
			return lockErr
		}
		if !locked {
			result.Skipped = true
			return nil
		}
		var err error
		result.TunnelEventsDeleted, err = s.deleteTunnelEvents(ctx, tx, now.Add(-cfg.TunnelEventsTTL), cfg.TunnelEventsKeep, cfg.BatchSize)
		if err != nil {
			return err
		}
		result.QuotaEventsDeleted, err = s.deleteQuotaEvents(ctx, tx, now.Add(-cfg.QuotaEventsTTL), cfg.BatchSize)
		if err != nil {
			return err
		}
		result.TerminalRunsDeleted, err = s.deleteTerminalRuns(ctx, tx, terminalRunRetentionRequest{
			cutoff:    now.Add(-cfg.TerminalRunsTTL),
			now:       now,
			batchSize: cfg.BatchSize,
		})
		if err != nil {
			return err
		}
		result.AccessGrantsDeleted, err = s.deleteExpiredAccessGrants(ctx, tx, now.Add(-cfg.AccessGrantsTTL), now, cfg.BatchSize)
		if err != nil {
			return err
		}
		deleted, err := tx.Exec(ctx, `DELETE FROM node_enrollment_receipts WHERE node_id IN (
		 SELECT r.node_id FROM node_enrollment_receipts r JOIN nodes n USING(node_id)
		 WHERE n.admitted_at + $1 * INTERVAL '1 second' <= clock_timestamp()
		 ORDER BY n.admitted_at,r.node_id LIMIT $2
		)`, int64(nodekernel.EnrollmentLifetime/time.Second), cfg.BatchSize)
		if err != nil {
			return err
		}
		result.EnrollmentReceiptsDeleted = deleted.RowsAffected()
		return nil
	})
	return result, err
}
