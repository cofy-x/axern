package pgservice

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
	serviceStatusBatchStageLockServices      = "lock_services"
	serviceStatusBatchStageLockAllocations   = "lock_allocations"
	serviceStatusBatchStageUpdateAllocations = "update_allocations"
	serviceStatusBatchStageProjectServices   = "project_services"
	serviceStatusBatchStageTotal             = "total"
)

func recordServiceStatusBatchStage(ctx context.Context, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		code := grpcstatus.Code(err)
		if code != codes.OK && code != codes.Unknown {
			errorClass = strings.ToLower(code.String())
		} else {
			errorClass = "error"
		}
	}
	sdkobs.DurationHistogram(ctrlobs.MetricServiceStatusBatchStageDuration.Name, ctrlobs.MetricServiceStatusBatchStageDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}
