package appservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func (c *controller) evaluateAutoscaling(ctx context.Context, current *servicev1.Service, now time.Time) (int, *servicev1.Service, error) {
	if current == nil {
		return 0, current, nil
	}
	eval, err := servicekernel.EvaluateAutoscaling(current, now)
	if err != nil {
		return int(current.GetReplicas()), current, err
	}
	if current.GetAutoscalingPolicy() == nil {
		return eval.Desired, current, nil
	}
	if autoscalingStatusEqual(current.GetAutoscalingStatus(), eval.Status) {
		return eval.Desired, current, nil
	}
	next, err := c.statuses.UpdateAutoscalingStatus(ctx, current.GetID(), eval.Status, now)
	if err != nil {
		return eval.Desired, current, err
	}
	if eval.TargetChanged {
		previousDesired := current.GetReplicas()
		if current.GetAutoscalingStatus() != nil {
			previousDesired = current.GetAutoscalingStatus().GetCurrentDesiredReplicas()
		}
		message := fmt.Sprintf(
			"autoscaling target changed: rule=%s previous=%d current=%d evaluated_at=%s",
			servicekernel.FirstNonEmpty(eval.Status.GetActiveScheduleName(), "manual"),
			previousDesired,
			eval.Status.GetCurrentDesiredReplicas(),
			eval.Status.GetLastEvaluatedAt().AsTime().UTC().Format(time.RFC3339),
		)
		if err := c.recordEvent(ctx, servicekernel.NewServiceEvent(
			next.GetID(),
			"",
			servicev1.ServiceEventType_SERVICE_EVENT_TYPE_AUTOSCALE_TARGET_CHANGED,
			servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED,
			commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
			message,
			now,
		)); err != nil {
			return eval.Desired, next, err
		}
	}
	return eval.Desired, next, nil
}

func autoscalingStatusEqual(a, b *servicev1.ServiceAutoscalingStatus) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.GetCurrentDesiredReplicas() == b.GetCurrentDesiredReplicas() &&
		a.GetEffectiveMinReplicas() == b.GetEffectiveMinReplicas() &&
		a.GetEffectiveMaxReplicas() == b.GetEffectiveMaxReplicas() &&
		strings.TrimSpace(a.GetActiveScheduleName()) == strings.TrimSpace(b.GetActiveScheduleName()) &&
		a.GetActiveScheduleReplicas() == b.GetActiveScheduleReplicas() &&
		a.GetLastAction() == b.GetLastAction() &&
		strings.TrimSpace(a.GetMessage()) == strings.TrimSpace(b.GetMessage()) &&
		a.GetLastEvaluatedAt().AsTime().UTC().Equal(b.GetLastEvaluatedAt().AsTime().UTC())
}
