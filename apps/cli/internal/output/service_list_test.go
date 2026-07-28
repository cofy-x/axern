package output

import (
	"strings"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderServiceTableUsesCompactDefaultColumns(t *testing.T) {
	var b strings.Builder
	now := time.Now().UTC()
	RenderServiceTable(&b, []*servicev1.Service{{
		ID:                "svc-1",
		Namespace:         "team-a",
		Status:            servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Replicas:          3,
		ReadyReplicas:     1,
		UnhealthyReplicas: 0,
		CreatedAt:         timestamppb.New(now.Add(-2 * time.Hour)),
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{MinReplicas: 1, MaxReplicas: 5},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 3,
			ActiveScheduleName:     "business-hours",
		},
		RolloutStatus: &servicev1.ServiceRolloutStatus{
			InProgress: true,
			Phase:      servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY,
		},
	}, {
		ID:            "svc-2",
		Namespace:     "team-b",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Replicas:      1,
		ReadyReplicas: 1,
		CreatedAt:     timestamppb.New(now.Add(-30 * time.Minute)),
	}}, ServiceListTableOptions{})
	out := b.String()
	for _, want := range []string{
		"ID",
		"NAMESPACE",
		"svc-1",
		"team-a",
		"reconciling",
		"1/3",
		"2h",
		"svc-2",
		"team-b",
		"ready",
		"1/1",
		"30m",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
	for _, unwanted := range []string{"AUTOSCALE", "ROLLOUT", "business-...:3", "waiting-ready"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("output %q contains default-only hidden column value %q", out, unwanted)
		}
	}
}

func TestRenderServiceTableWideWithLabelsIncludesOperationalColumns(t *testing.T) {
	var b strings.Builder
	now := time.Now().UTC()
	RenderServiceTable(&b, []*servicev1.Service{{
		ID:                "svc-1",
		Namespace:         "team-a",
		Status:            servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Replicas:          3,
		ReadyReplicas:     1,
		UnhealthyReplicas: 2,
		CreatedAt:         timestamppb.New(now.Add(-2 * time.Hour)),
		Labels:            map[string]string{"team": "platform", "env": "prod"},
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{MinReplicas: 1, MaxReplicas: 5},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 3,
			ActiveScheduleName:     "business-hours",
		},
		RolloutStatus: &servicev1.ServiceRolloutStatus{
			InProgress: true,
			Phase:      servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY,
		},
	}}, ServiceListTableOptions{Wide: true, ShowLabels: true})
	out := b.String()
	for _, want := range []string{
		"UNHEALTHY",
		"AUTOSCALE",
		"ROLLOUT",
		"LABELS",
		"2",
		"business-...:3",
		"waiting-ready",
		"env=prod,team=platform",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}
