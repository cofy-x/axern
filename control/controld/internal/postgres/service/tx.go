package pgservice

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) withTx(ctx context.Context, fn func(tx pgx.Tx) error) (retErr error) {
	totalStarted := time.Now()
	defer func() {
		recordServiceTransactionStage(ctx, serviceTransactionStageTotal, totalStarted, retErr)
	}()

	stageStarted := time.Now()
	tx, err := s.db.Pool().Begin(ctx)
	recordServiceTransactionStage(ctx, serviceTransactionStageBegin, stageStarted, err)
	if err != nil {
		return fmt.Errorf("begin service tx: %w", err)
	}
	defer tx.Rollback(ctx)
	stageStarted = time.Now()
	err = fn(tx)
	recordServiceTransactionStage(ctx, serviceTransactionStageBody, stageStarted, err)
	if err != nil {
		if postgres.ShouldCommitError(err) {
			stageStarted = time.Now()
			commit := tx.Commit(ctx)
			recordServiceTransactionStage(ctx, serviceTransactionStageCommit, stageStarted, commit)
			if commit != nil {
				return fmt.Errorf("commit service tx: %w", commit)
			}
		}
		return err
	}
	stageStarted = time.Now()
	err = tx.Commit(ctx)
	recordServiceTransactionStage(ctx, serviceTransactionStageCommit, stageStarted, err)
	if err != nil {
		return fmt.Errorf("commit service tx: %w", err)
	}
	return nil
}
