package main

import (
	"context"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	controldobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

const (
	resourceServiceEvents       = "service_events"
	resourceTunnelEvents        = "tunnel_events"
	resourceQuotaEvents         = "quota_events"
	resourceServiceAllocations  = "service_allocations"
	resourceTerminalRuns        = "terminal_runs"
	resourceLeases              = "leases"
	resourceFunctionEvents      = "function_events"
	resourceFunctionInvocations = "function_invocations"
	resourceFunctionIdempotency = "function_idempotency"
)

type cleanupController interface {
	Cleanup(ctx context.Context, now time.Time) (retentionkernel.Result, error)
}

type runner struct {
	controller cleanupController
	timeout    time.Duration
	now        func() time.Time
}

func newRunner(controller cleanupController, timeout time.Duration, now func() time.Time) *runner {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &runner{controller: controller, timeout: timeout, now: now}
}

func (r *runner) RunOnce(ctx context.Context) error {
	if r == nil || r.controller == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:     controldobs.SpanRetentionCleanup,
		Duration: controldobs.MetricRetentionDuration,
	})
	result, err := r.controller.Cleanup(ctx, r.now())
	if result.Skipped {
		op.SetResult(sdkobs.ResultSkipped)
	}
	if err != nil {
		op.SetErrorClass("cleanup_error")
	}
	op.End(err)

	recordDeleted(ctx, resourceServiceEvents, result.ServiceEventsDeleted)
	recordDeleted(ctx, resourceTunnelEvents, result.TunnelEventsDeleted)
	recordDeleted(ctx, resourceQuotaEvents, result.QuotaEventsDeleted)
	recordDeleted(ctx, resourceServiceAllocations, result.ServiceAllocationsDeleted)
	recordDeleted(ctx, resourceTerminalRuns, result.TerminalRunsDeleted)
	recordDeleted(ctx, resourceLeases, result.LeasesDeleted)
	recordDeleted(ctx, resourceFunctionEvents, result.FunctionEventsDeleted)
	recordDeleted(ctx, resourceFunctionInvocations, result.FunctionInvocationsDeleted)
	recordDeleted(ctx, resourceFunctionIdempotency, result.FunctionIdempotencyDeleted)
	logResult(result, err)
	return err
}

func recordDeleted(ctx context.Context, resource string, count int64) {
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

func logResult(result retentionkernel.Result, err error) {
	fields := logrus.Fields{
		"events_deleted":               result.ServiceEventsDeleted,
		"tunnel_events_deleted":        result.TunnelEventsDeleted,
		"quota_events_deleted":         result.QuotaEventsDeleted,
		"service_allocations_deleted":  result.ServiceAllocationsDeleted,
		"runs_deleted":                 result.TerminalRunsDeleted,
		"leases_deleted":               result.LeasesDeleted,
		"function_events_deleted":      result.FunctionEventsDeleted,
		"function_invocations_deleted": result.FunctionInvocationsDeleted,
		"function_idempotency_deleted": result.FunctionIdempotencyDeleted,
		"duration":                     result.Duration.String(),
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
