package pgrollout

import (
	"context"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func recordRolloutWorkNotification(action string, woken int) {
	result := "unmatched"
	if woken > 0 {
		result = "matched"
	}
	ctx := context.Background()
	sdkobs.Int64Counter(ctrlobs.MetricRolloutWorkNotificationTotal.Name, ctrlobs.MetricRolloutWorkNotificationTotal.Description).Add(ctx, 1,
		attribute.String(sdkobs.AttrTrigger, action),
		attribute.String(sdkobs.AttrResult, result),
	)
	if woken > 0 {
		sdkobs.Int64Counter(ctrlobs.MetricRolloutWorkWakeupTotal.Name, ctrlobs.MetricRolloutWorkWakeupTotal.Description).Add(ctx, int64(woken),
			attribute.String(sdkobs.AttrTrigger, action),
		)
	}
}
