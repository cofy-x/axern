package pgservice

import (
	"context"
	"time"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	serviceTransactionStageBegin  = "begin"
	serviceTransactionStageBody   = "body"
	serviceTransactionStageCommit = "commit"
	serviceTransactionStageTotal  = "total"
)

func recordServiceTransactionStage(ctx context.Context, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = "error"
	}
	sdkobs.DurationHistogram(
		ctrlobs.MetricServiceTransactionStageDuration.Name,
		ctrlobs.MetricServiceTransactionStageDuration.Description,
	).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}
