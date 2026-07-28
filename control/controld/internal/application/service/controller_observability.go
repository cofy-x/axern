package appservice

import (
	"context"
	"strings"
	"time"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	serviceReplicaPathScaleUp            = "scale_up"
	serviceReplicaPathRolloutReplacement = "rollout_replacement"
	serviceReplicaPathReconcileCreate    = "reconcile_create"
	serviceReplicaPathReconcileDelete    = "reconcile_delete"

	serviceReplicaStageSelectCandidates         = "select_candidates"
	serviceReplicaStageFilterStorageCandidates  = "filter_storage_candidates"
	serviceReplicaStageAdmitAllocation          = "admit_allocation"
	serviceReplicaStageReserveStorage           = "reserve_storage"
	serviceReplicaStageNodeCreateAllocation     = "node_create_allocation"
	serviceReplicaStageReportStoragePublished   = "report_storage_published"
	serviceReplicaStageCompleteAllocationCreate = "complete_allocation_create"

	serviceAllocationQueueStageClaimStore     = "claim_store"
	serviceAllocationQueueStageDueLag         = "due_lag"
	serviceAllocationQueueStageClaimWait      = "claim_wait"
	serviceAllocationQueueStageDispatcherWait = "dispatcher_wait"
	serviceAllocationQueueStageTotal          = "total"

	serviceReconcileStageQueueWait = "queue_wait"
	serviceReconcileStageLockWait  = "lock_wait"
	serviceReconcileStageSync      = "sync"
	serviceReconcileStageTotal     = "total"
)

var serviceAllocationQueueHistogramBuckets = []float64{
	0.0005,
	0.001,
	0.0025,
	0.005,
	0.01,
	0.025,
	0.05,
	0.075,
	0.1,
	0.15,
	0.2,
	0.25,
	0.3,
	0.35,
	0.4,
	0.45,
	0.5,
	0.75,
	1,
	2,
	5,
	10,
}

func (c *controller) recordReplicaStage(ctx context.Context, path, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = errorClassForStage(err)
	}
	sdkobs.DurationHistogram(ctrlobs.MetricServiceReplicaStageDuration.Name, ctrlobs.MetricServiceReplicaStageDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrPath, path),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func (c *controller) recordAllocationDispatcherCurrent(ctx context.Context, active, pending int) {
	gauge := sdkobs.Float64Gauge(ctrlobs.MetricServiceAllocationDispatcherCurrent.Name, ctrlobs.MetricServiceAllocationDispatcherCurrent.Description)
	gauge.Record(ctx, float64(active), attribute.String(sdkobs.AttrState, "active"))
	gauge.Record(ctx, float64(pending), attribute.String(sdkobs.AttrState, "claimed_pending"))
}

func (c *controller) recordServiceAllocationQueue(ctx context.Context, path, stage string, elapsed time.Duration, err error) {
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = errorClassForStage(err)
	}
	sdkobs.DurationHistogramWithBuckets(
		ctrlobs.MetricServiceAllocationQueueDuration.Name,
		ctrlobs.MetricServiceAllocationQueueDuration.Description,
		serviceAllocationQueueHistogramBuckets...,
	).RecordDuration(ctx, elapsed,
		attribute.String(sdkobs.AttrPath, path),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func (c *controller) recordReconcileStage(ctx context.Context, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = errorClassForStage(err)
	}
	sdkobs.DurationHistogram(ctrlobs.MetricServiceReconcileStageDuration.Name, ctrlobs.MetricServiceReconcileStageDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func errorClassForStage(err error) string {
	if err == nil {
		return ""
	}
	code := grpcstatus.Code(err)
	if code != codes.OK && code != codes.Unknown {
		return strings.ToLower(code.String())
	}
	return "error"
}
