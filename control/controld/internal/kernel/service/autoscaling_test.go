package servicekernel

import (
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestValidateAndNormalizeAutoscalingPolicyRejectsInvalidValues(t *testing.T) {
	if _, err := ValidateAndNormalizeAutoscalingPolicy(&servicev1.ServiceAutoscalingPolicy{MinReplicas: -1, MaxReplicas: 1}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("min_replicas code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
	if _, err := ValidateAndNormalizeAutoscalingPolicy(&servicev1.ServiceAutoscalingPolicy{MinReplicas: 2, MaxReplicas: 1}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("min/max code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
	if _, err := ValidateAndNormalizeAutoscalingPolicy(&servicev1.ServiceAutoscalingPolicy{
		MinReplicas: 1,
		MaxReplicas: 5,
		Schedules: []*servicev1.ServiceAutoscalingSchedule{{
			Name:     "bad",
			CronUtc:  "not-a-cron",
			Replicas: 3,
		}},
	}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("cron code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestEvaluateAutoscalingFallsBackToManualReplicasWhenNoRuleMatches(t *testing.T) {
	now := time.Date(2026, 4, 27, 8, 30, 0, 0, time.UTC)
	service := &servicev1.Service{
		Replicas: 2,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
	}
	eval, err := evaluateAutoscaling(service, now)
	if err != nil {
		t.Fatalf("evaluateAutoscaling returned error: %v", err)
	}
	if eval.Desired != 2 {
		t.Fatalf("desired = %d, want 2", eval.Desired)
	}
	if eval.Status.GetActiveScheduleName() != "" {
		t.Fatalf("active schedule = %q, want empty", eval.Status.GetActiveScheduleName())
	}
}

func TestEvaluateAutoscalingChoosesHighestReplicaMatchingRule(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	service := &servicev1.Service{
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 6,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{
				{Name: "small", CronUtc: "* 9-17 * * 1-5", Replicas: 2},
				{Name: "large", CronUtc: "* 9-17 * * 1-5", Replicas: 4},
			},
		},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 1,
		},
	}
	eval, err := evaluateAutoscaling(service, now)
	if err != nil {
		t.Fatalf("evaluateAutoscaling returned error: %v", err)
	}
	if eval.Desired != 4 {
		t.Fatalf("desired = %d, want 4", eval.Desired)
	}
	if eval.Status.GetActiveScheduleName() != "large" {
		t.Fatalf("active schedule = %q, want large", eval.Status.GetActiveScheduleName())
	}
	if !eval.TargetChanged {
		t.Fatal("targetChanged = false, want true")
	}
	if eval.Status.GetLastAction() != servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_UP {
		t.Fatalf("last action = %v, want scaled up", eval.Status.GetLastAction())
	}
}

func TestEvaluateAutoscalingFirstActiveScheduleUsesManualReplicasAsBaseline(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	service := &servicev1.Service{
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
	}
	eval, err := evaluateAutoscaling(service, now)
	if err != nil {
		t.Fatalf("evaluateAutoscaling returned error: %v", err)
	}
	if !eval.TargetChanged {
		t.Fatal("targetChanged = false, want true")
	}
	if eval.Status.GetLastAction() != servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_UP {
		t.Fatalf("last action = %v, want scaled up", eval.Status.GetLastAction())
	}
}

func TestEvaluateAutoscalingReturnsToManualReplicasWhenScheduleEnds(t *testing.T) {
	now := time.Date(2026, 4, 27, 18, 0, 0, 0, time.UTC)
	service := &servicev1.Service{
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 3,
			ActiveScheduleName:     "business",
			ActiveScheduleReplicas: 3,
		},
	}
	eval, err := evaluateAutoscaling(service, now)
	if err != nil {
		t.Fatalf("evaluateAutoscaling returned error: %v", err)
	}
	if eval.Desired != 1 {
		t.Fatalf("desired = %d, want manual replicas 1", eval.Desired)
	}
	if eval.Status.GetActiveScheduleName() != "" {
		t.Fatalf("active schedule = %q, want empty after schedule window", eval.Status.GetActiveScheduleName())
	}
	if !eval.TargetChanged {
		t.Fatal("targetChanged = false, want true when returning to manual replicas")
	}
	if eval.Status.GetLastAction() != servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_DOWN {
		t.Fatalf("last action = %v, want scaled down", eval.Status.GetLastAction())
	}
}
