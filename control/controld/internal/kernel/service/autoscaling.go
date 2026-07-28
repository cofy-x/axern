package servicekernel

import (
	"fmt"
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/robfig/cron/v3"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AutoscalingEvaluation struct {
	Desired       int
	Status        *servicev1.ServiceAutoscalingStatus
	TargetChanged bool
}

func autoscalingCronParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

func evaluateAutoscaling(service *servicev1.Service, now time.Time) (*AutoscalingEvaluation, error) {
	if service == nil {
		return &AutoscalingEvaluation{}, nil
	}
	evaluatedAt := now.UTC().Truncate(time.Minute)
	policy := cloneAutoscalingPolicy(service.GetAutoscalingPolicy())
	if policy == nil {
		return &AutoscalingEvaluation{
			Desired: int(service.GetReplicas()),
		}, nil
	}

	active, err := selectAutoscalingSchedule(policy, evaluatedAt)
	if err != nil {
		return nil, err
	}
	desired := int(service.GetReplicas())
	if active != nil {
		desired = int(active.GetReplicas())
	}

	status := &servicev1.ServiceAutoscalingStatus{
		CurrentDesiredReplicas: int32(desired),
		EffectiveMinReplicas:   policy.GetMinReplicas(),
		EffectiveMaxReplicas:   policy.GetMaxReplicas(),
		LastEvaluatedAt:        timestamppb.New(evaluatedAt),
		LastAction:             servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_NO_CHANGE,
	}
	if active != nil {
		status.ActiveScheduleName = active.GetName()
		status.ActiveScheduleReplicas = active.GetReplicas()
		status.Message = fmt.Sprintf("active schedule %q targets %d replicas", active.GetName(), active.GetReplicas())
	} else {
		status.Message = fmt.Sprintf("no active autoscaling schedule; using manual replicas=%d", service.GetReplicas())
	}

	prev := service.GetAutoscalingStatus()
	previousDesired := service.GetReplicas()
	if prev != nil {
		previousDesired = prev.GetCurrentDesiredReplicas()
	}
	targetChanged := previousDesired != status.GetCurrentDesiredReplicas()
	if targetChanged {
		if status.GetCurrentDesiredReplicas() > previousDesired {
			status.LastAction = servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_UP
		} else {
			status.LastAction = servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_DOWN
		}
	} else {
		status.LastAction = servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_NO_CHANGE
	}

	return &AutoscalingEvaluation{
		Desired:       desired,
		Status:        normalizeAutoscalingStatus(status),
		TargetChanged: targetChanged,
	}, nil
}

func EvaluateAutoscaling(service *servicev1.Service, now time.Time) (*AutoscalingEvaluation, error) {
	return evaluateAutoscaling(service, now)
}

func selectAutoscalingSchedule(policy *servicev1.ServiceAutoscalingPolicy, now time.Time) (*servicev1.ServiceAutoscalingSchedule, error) {
	if policy == nil || len(policy.GetSchedules()) == 0 {
		return nil, nil
	}
	now = now.UTC().Truncate(time.Minute)
	prev := now.Add(-time.Minute)
	parser := autoscalingCronParser()

	var selected *servicev1.ServiceAutoscalingSchedule
	for _, schedule := range policy.GetSchedules() {
		if schedule == nil {
			continue
		}
		spec, err := parser.Parse(schedule.GetCronUtc())
		if err != nil {
			return nil, fmt.Errorf("parse autoscaling cron %q: %w", schedule.GetCronUtc(), err)
		}
		if !spec.Next(prev).Equal(now) {
			continue
		}
		if selected == nil ||
			schedule.GetReplicas() > selected.GetReplicas() ||
			(schedule.GetReplicas() == selected.GetReplicas() && strings.Compare(schedule.GetName(), selected.GetName()) < 0) {
			selected = schedule
		}
	}
	return selected, nil
}
