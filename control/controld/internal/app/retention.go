package app

import (
	"context"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	controldobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

const retentionTimeout = 30 * time.Second

type retentionCleanupController interface {
	Cleanup(context.Context, time.Time) (retentionkernel.Result, error)
}

func (a *App) startRetentionWorker(interval time.Duration) {
	if a.retention == nil {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ctx := a.backgroundReconcileContext()
		for {
			select {
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				_ = a.cleanupRetainedHistory(ctx)
			case <-ctx.Done():
				return
			case <-a.stopCh:
				return
			}
		}
	}()
}

func (a *App) cleanupRetainedHistory(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, retentionTimeout)
	defer cancel()
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:     controldobs.SpanRetentionCleanup,
		Duration: controldobs.MetricRetentionDuration,
	})
	result, err := a.retention.Cleanup(ctx, a.now())
	if result.Skipped {
		op.SetResult(sdkobs.ResultSkipped)
	}
	if err != nil {
		op.SetErrorClass("cleanup_error")
	}
	op.End(err)

	recordRetentionDeleted(ctx, "tunnel_events", result.TunnelEventsDeleted)
	recordRetentionDeleted(ctx, "quota_events", result.QuotaEventsDeleted)
	recordRetentionDeleted(ctx, "terminal_runs", result.TerminalRunsDeleted)
	recordRetentionDeleted(ctx, "allocation_access_grants", result.AccessGrantsDeleted)
	recordRetentionDeleted(ctx, "node_enrollment_receipts", result.EnrollmentReceiptsDeleted)
	logRetentionResult(result, err)
	return err
}

func recordRetentionDeleted(ctx context.Context, resource string, count int64) {
	if count <= 0 {
		return
	}
	sdkobs.Int64Counter(controldobs.MetricRetentionDeletedTotal.Name, controldobs.MetricRetentionDeletedTotal.Description).Add(
		ctx,
		count,
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrResult, sdkobs.ResultOK),
	)
}

func logRetentionResult(result retentionkernel.Result, err error) {
	fields := logrus.Fields{
		"tunnel_events_deleted":       result.TunnelEventsDeleted,
		"quota_events_deleted":        result.QuotaEventsDeleted,
		"runs_deleted":                result.TerminalRunsDeleted,
		"access_grants_deleted":       result.AccessGrantsDeleted,
		"enrollment_receipts_deleted": result.EnrollmentReceiptsDeleted,
		"duration":                    result.Duration.String(),
	}
	if err != nil {
		logrus.WithError(err).WithFields(fields).Warn("retention cleanup failed")
		return
	}
	if result.Skipped {
		logrus.WithFields(fields).Debug("retention cleanup skipped")
		return
	}
	if result.TotalDeleted() > 0 {
		logrus.WithFields(fields).Info("retention cleanup completed")
	}
}
