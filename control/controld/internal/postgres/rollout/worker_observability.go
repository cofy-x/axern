package pgrollout

import (
	"context"
	"time"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	workClaimResultClaimed         = "claimed"
	workClaimResultEmpty           = "empty"
	workClaimResultCapacityBlocked = "capacity_blocked"
	workClaimResultError           = "error"
)

func recordRolloutWorkClaim(ctx context.Context, result string, started time.Time, err error) {
	if err != nil {
		result = workClaimResultError
	}
	attributes := []attribute.KeyValue{attribute.String(sdkobs.AttrResult, result)}
	sdkobs.Int64Counter(ctrlobs.MetricRolloutWorkClaimTotal.Name, ctrlobs.MetricRolloutWorkClaimTotal.Description).Add(ctx, 1, attributes...)
	sdkobs.DurationHistogram(ctrlobs.MetricRolloutWorkClaimDuration.Name, ctrlobs.MetricRolloutWorkClaimDuration.Description).RecordDuration(ctx, time.Since(started), attributes...)
}

func recordRolloutWorkClaimLag(ctx context.Context, dueAt, claimedAt time.Time, kind string) {
	lag := claimedAt.Sub(dueAt)
	if lag < 0 {
		lag = 0
	}
	sdkobs.DurationHistogram(ctrlobs.MetricRolloutWorkClaimLag.Name, ctrlobs.MetricRolloutWorkClaimLag.Description).RecordDuration(ctx, lag,
		attribute.String(sdkobs.AttrKind, kind),
	)
}
