package pgretention

import (
	"context"
	"time"

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
		result.ServiceEventsDeleted, err = s.deleteServiceEvents(ctx, tx, now.Add(-cfg.ServiceEventsTTL), cfg.ServiceEventsKeep, cfg.BatchSize)
		if err != nil {
			return err
		}
		result.TunnelEventsDeleted, err = s.deleteTunnelEvents(ctx, tx, now.Add(-cfg.TunnelEventsTTL), cfg.TunnelEventsKeep, cfg.BatchSize)
		if err != nil {
			return err
		}
		result.QuotaEventsDeleted, err = s.deleteQuotaEvents(ctx, tx, now.Add(-cfg.QuotaEventsTTL), cfg.BatchSize)
		if err != nil {
			return err
		}
		result.ServiceAllocationsDeleted, err = s.deleteServiceAllocations(ctx, tx, serviceAllocationRetentionRequest{
			cutoff:    now.Add(-cfg.ServiceReplicasTTL),
			keep:      cfg.ServiceReplicasKeep,
			batchSize: cfg.BatchSize,
			now:       now,
		})
		if err != nil {
			return err
		}
		result.TerminalRunsDeleted, err = s.deleteTerminalRuns(ctx, tx, terminalRunRetentionRequest{
			cutoff:    now.Add(-cfg.TerminalRunsTTL),
			batchSize: cfg.BatchSize,
			now:       now,
		})
		if err != nil {
			return err
		}
		result.LeasesDeleted, err = s.deleteExpiredLeases(ctx, tx, now.Add(-cfg.LeasesTTL), now, cfg.BatchSize)
		if err != nil {
			return err
		}
		return nil
	})
	return result, err
}
