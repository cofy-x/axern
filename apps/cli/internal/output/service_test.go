package output

import (
	"fmt"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderServiceIncludesRolloutSummary(t *testing.T) {
	var b strings.Builder
	RenderService(&b, &servicev1.Service{
		ID:                "svc-1",
		Namespace:         "default",
		EnvironmentID:     "env-1",
		Status:            servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Replicas:          2,
		ReadyReplicas:     1,
		UnhealthyReplicas: 0,
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{
				Http: &servicev1.HttpProbe{Port: 8080, Path: "/readyz"},
			},
			InitialDelay:     durationpb.New(100 * time.Millisecond),
			Period:           durationpb.New(750 * time.Millisecond),
			Timeout:          durationpb.New(250 * time.Millisecond),
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
		LivenessProbe: &servicev1.ServiceProbe{
			Action:           &servicev1.ServiceProbe_Tcp{Tcp: &servicev1.TcpProbe{Port: 9090}},
			Period:           &durationpb.Duration{Seconds: 10},
			Timeout:          &durationpb.Duration{Seconds: 2},
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
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
			EffectiveMinReplicas:   1,
			EffectiveMaxReplicas:   5,
			ActiveScheduleName:     "business",
			ActiveScheduleReplicas: 3,
			LastAction:             servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_UP,
			LastEvaluatedAt:        timestamppb.Now(),
			Message:                "active schedule \"business\" targets 3 replicas",
		},
		RolloutPolicy: &servicev1.ServiceRolloutPolicy{MaxSurge: 1, MaxUnavailable: 0},
		RolloutStatus: &servicev1.ServiceRolloutStatus{
			InProgress:           true,
			Phase:                servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY,
			CurrentReplicas:      3,
			UpdatedReadyReplicas: 1,
			OutdatedReplicas:     2,
			DiagnosticCode:       commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR,
			DiagnosticMessage:    "check the referenced docker-config-json secret",
		},
		Config: &commonv1.ExecutionConfig{
			VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name:     "data",
				Target:   "/var/lib/app",
				Readonly: true,
				Options:  []string{"rbind"},
			}},
		},
		Message: "rolling update in progress",
	})
	out := b.String()
	for _, want := range []string{
		"ID: svc-1",
		"Status: reconciling",
		"Readiness Probe: http port=8080 path=/readyz initial_delay=100ms period=750ms timeout=250ms success_threshold=1 failure_threshold=1",
		"Liveness Probe: tcp port=9090 initial_delay=0s period=10s timeout=2s success_threshold=1 failure_threshold=3",
		"Volumes: data:/var/lib/app:ro,rbind",
		"Autoscaling Policy: min=1 max=5 schedules=1",
		"- business cron=* 9-17 * * 1-5 replicas=3",
		"Autoscaling: current_desired=3 min=1 max=5 active=business target=3 action=scaled-up",
		"Autoscaling Detail: active schedule \"business\" targets 3 replicas",
		"Rollout Policy: max_surge=1 max_unavailable=0",
		"Rollout: in_progress=true phase=waiting-for-updated-ready current=3 updated_ready=1 outdated=2",
		"Rollout Diagnostic: registry-auth-error",
		"Rollout Detail: check the referenced docker-config-json secret",
		"Message: rolling update in progress",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceHighlightsAdmissionBlockedMessage(t *testing.T) {
	var b strings.Builder
	RenderService(&b, &servicev1.Service{
		ID:            "svc-quota",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Replicas:      1,
		Message:       "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a cpu requested_milli=500 reserved_milli=0 limit_milli=100 available_milli=100",
	})
	out := b.String()
	for _, want := range []string{
		"Status: degraded",
		"Diagnostic: admission-blocked",
		"Admission: namespace quota exceeded",
		"Message: rpc error: code = ResourceExhausted desc = namespace quota exceeded",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceHighlightsCapacityAdmissionBlockedMessage(t *testing.T) {
	var b strings.Builder
	RenderService(&b, &servicev1.Service{
		ID:            "svc-cpu",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Replicas:      1,
		Message:       "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_cpu",
	})
	out := b.String()
	for _, want := range []string{
		"Diagnostic: admission-blocked",
		"Admission: node CPU capacity exhausted",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceDoesNotTreatNodeSelectionAsAdmissionBlocked(t *testing.T) {
	var b strings.Builder
	RenderService(&b, &servicev1.Service{
		ID:            "svc-placement",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Replicas:      1,
		Message:       "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
	})
	out := b.String()
	for _, unexpected := range []string{
		"Diagnostic: admission-blocked",
		"Admission:",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("output %q unexpectedly contains %q", out, unexpected)
		}
	}
}

func TestRenderServiceDescribeIncludesMetadataAndLatestEvent(t *testing.T) {
	var b strings.Builder
	RenderServiceDescribe(&b, &servicev1.Service{
		ID:                "svc-1",
		Namespace:         "default",
		EnvironmentID:     "env-1",
		Status:            servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Replicas:          2,
		ReadyReplicas:     1,
		UnhealthyReplicas: 1,
		Labels:            map[string]string{"team": "platform", "env": "prod"},
		Version:           7,
		CreatedAt:         timestamppb.New(time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)),
		UpdatedAt:         timestamppb.New(time.Date(2026, time.May, 1, 12, 5, 0, 0, time.UTC)),
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{
				Http: &servicev1.HttpProbe{Port: 8080, Path: "/readyz"},
			},
			Period:           &durationpb.Duration{Seconds: 5},
			Timeout:          &durationpb.Duration{Seconds: 2},
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
		RolloutStatus: &servicev1.ServiceRolloutStatus{
			InProgress:           true,
			Phase:                servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED,
			CurrentReplicas:      2,
			UpdatedReadyReplicas: 1,
			OutdatedReplicas:     1,
			DiagnosticCode:       commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED,
			DiagnosticMessage:    "readiness probe failed",
		},
		Message: "rollout blocked",
	}, &servicev1.ServiceEvent{
		ID:             "svcevt-1",
		ServiceID:      "svc-1",
		ReplicaID:      "alloc-1",
		Type:           servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED,
		Phase:          servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED,
		Message:        "replacement blocked by readiness probe",
	})
	out := b.String()
	for _, want := range []string{
		"Labels: env=prod,team=platform",
		"Version: 7",
		"Created At: 2026-05-01T12:00:00Z",
		"Updated At: 2026-05-01T12:05:00Z",
		"Readiness Probe: http port=8080 path=/readyz",
		"Rollout: in_progress=true phase=blocked current=2 updated_ready=1 outdated=1",
		"Rollout Diagnostic: liveness-probe-failed",
		"Latest Event: replacement-blocked phase=blocked diagnostic=liveness-probe-failed replica=alloc-1",
		"Message: rollout blocked",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceOmitsActionlessProbeAndEmptyAutoscalingPolicy(t *testing.T) {
	var b strings.Builder
	RenderService(&b, &servicev1.Service{
		ID:            "svc-1",
		Namespace:     "default",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		LivenessProbe: &servicev1.ServiceProbe{
			Period:           &durationpb.Duration{Seconds: 5},
			Timeout:          &durationpb.Duration{Seconds: 2},
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{},
	})
	out := b.String()
	for _, unexpected := range []string{"Liveness Probe: unknown", "Autoscaling Policy: min=0 max=0"} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("output %q unexpectedly contains %q", out, unexpected)
		}
	}
}

func TestRenderServiceDescribeIncludesDeletionLifecycle(t *testing.T) {
	var b strings.Builder
	RenderServiceDescribe(&b, &servicev1.Service{
		ID:     "svc-deleting",
		Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
			ClaimIds:          []string{"claim-a", "claim-b"},
			Message:           "waiting for volume cleanup",
			CompletedAt:       timestamppb.New(time.Date(2026, time.August, 13, 8, 30, 0, 0, time.UTC)),
		},
	}, nil)
	out := b.String()
	for _, want := range []string{
		"Status: deleting",
		"Deletion: phase=reclaiming-volumes volume_disposition=delete",
		"Deletion Claims: claim-a,claim-b",
		"Deletion Completed At: 2026-08-13T08:30:00Z",
		"Deletion Message: waiting for volume cleanup",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceDeletionResultDistinguishesRequestedAndComplete(t *testing.T) {
	tests := []struct {
		name    string
		service *servicev1.Service
		want    string
	}{
		{
			name: "requested",
			service: &servicev1.Service{ID: "svc-1", DeletionStatus: &servicev1.ServiceDeletionStatus{
				Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
			}},
			want: "Service deletion requested: svc-1 (phase=releasing-allocations)\n",
		},
		{
			name: "complete",
			service: &servicev1.Service{ID: "svc-1", DeletionStatus: &servicev1.ServiceDeletionStatus{
				Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
			}},
			want: "Service deleted: svc-1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			RenderServiceDeletionResult(&b, tc.service)
			if got := b.String(); got != tc.want {
				t.Fatalf("RenderServiceDeletionResult() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatRelativeAge(t *testing.T) {
	base := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		from time.Time
		to   time.Time
		want string
	}{
		{from: base.Add(-20 * time.Second), to: base, want: "just-now"},
		{from: base.Add(-5 * time.Minute), to: base, want: "5m"},
		{from: base.Add(-2 * time.Hour), to: base, want: "2h"},
		{from: base.Add(-49 * time.Hour), to: base, want: "2d"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_to_%s", tc.from.Format(time.RFC3339), tc.to.Format(time.RFC3339)), func(t *testing.T) {
			if got := FormatRelativeAge(tc.from, tc.to); got != tc.want {
				t.Fatalf("FormatRelativeAge(%v, %v) = %q, want %q", tc.from, tc.to, got, tc.want)
			}
		})
	}
}
