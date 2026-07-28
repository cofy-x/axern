package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPrintServiceListJSONUsesStableProbeShape(t *testing.T) {
	var b strings.Builder
	err := PrintServiceListJSON(&b, &servicev1.ListServicesResponse{
		Services: []*servicev1.Service{{
			ID:        "svc-1",
			Namespace: "default",
			Status:    servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Config: &commonv1.ExecutionConfig{
				Ports: []*commonv1.PortSpec{{
					Name:          "http",
					Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
					ContainerPort: 8080,
				}},
				Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT},
			},
			AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{},
			ReadinessProbe: &servicev1.ServiceProbe{
				Action: &servicev1.ServiceProbe_Http{
					Http: &servicev1.HttpProbe{
						Port:   8080,
						Path:   "/readyz",
						Scheme: servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTP,
					},
				},
				InitialDelay:     durationpb.New(100 * time.Millisecond),
				Period:           durationpb.New(750 * time.Millisecond),
				Timeout:          durationpb.New(250 * time.Millisecond),
				SuccessThreshold: 1,
				FailureThreshold: 1,
			},
			LivenessProbe: &servicev1.ServiceProbe{
				Period:           &durationpb.Duration{Seconds: 10},
				Timeout:          &durationpb.Duration{Seconds: 3},
				SuccessThreshold: 1,
				FailureThreshold: 3,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Services []struct {
			Status string `json:"status"`
			Config struct {
				Ports []struct {
					Protocol string `json:"protocol"`
				} `json:"ports"`
				Network struct {
					Mode string `json:"mode"`
				} `json:"network"`
			} `json:"config"`
			AutoscalingPolicy json.RawMessage `json:"autoscaling_policy"`
			ReadinessProbe    json.RawMessage `json:"readiness_probe"`
			LivenessProbe     json.RawMessage `json:"liveness_probe"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 1 {
		t.Fatalf("services len = %d, want 1", len(got.Services))
	}
	if got.Services[0].Status != "ready" {
		t.Fatalf("status = %q, want ready", got.Services[0].Status)
	}
	if got.Services[0].Config.Ports[0].Protocol != "tcp" || got.Services[0].Config.Network.Mode != "default" {
		t.Fatalf("config = %#v, want stable string enum labels", got.Services[0].Config)
	}
	if len(got.Services[0].AutoscalingPolicy) != 0 {
		t.Fatalf("autoscaling_policy = %s, want omitted for empty policy", got.Services[0].AutoscalingPolicy)
	}
	var readiness map[string]any
	if err := json.Unmarshal(got.Services[0].ReadinessProbe, &readiness); err != nil {
		t.Fatal(err)
	}
	if readiness["type"] != "http" || readiness["scheme"] != "http" || readiness["path"] != "/readyz" {
		t.Fatalf("readiness probe = %#v, want stable http shape", readiness)
	}
	if readiness["initial_delay"] != "100ms" || readiness["period"] != "750ms" || readiness["timeout"] != "250ms" {
		t.Fatalf("readiness probe durations = %#v, want millisecond precision", readiness)
	}
	if string(got.Services[0].LivenessProbe) != "null" {
		t.Fatalf("liveness probe = %s, want null for actionless probe", got.Services[0].LivenessProbe)
	}
	for _, unexpected := range []string{"Action", "Http", "PORT_PROTOCOL", "NETWORK_MODE"} {
		if strings.Contains(b.String(), unexpected) {
			t.Fatalf("service JSON leaked protobuf internals %q: %s", unexpected, b.String())
		}
	}
}

func TestPrintServiceDescribeJSONIncludesLatestEvent(t *testing.T) {
	var b strings.Builder
	err := PrintServiceDescribeJSON(&b, &servicev1.Service{
		ID:        "svc-1",
		Namespace: "default",
		Status:    servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Labels:    map[string]string{"team": "platform"},
	}, &servicev1.ServiceEvent{
		ID:             "svcevt-1",
		ServiceID:      "svc-1",
		ReplicaID:      "alloc-1",
		Type:           servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED,
		Phase:          servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR,
		Message:        "secret projection failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Service struct {
			ID     string            `json:"id"`
			Status string            `json:"status"`
			Labels map[string]string `json:"labels"`
		} `json:"service"`
		LatestEvent struct {
			ID             string `json:"id"`
			Type           string `json:"type"`
			Phase          string `json:"phase"`
			DiagnosticCode string `json:"diagnostic_code"`
			ReplicaID      string `json:"replica_id"`
		} `json:"latest_event"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Service.ID != "svc-1" || got.Service.Status != "degraded" || got.Service.Labels["team"] != "platform" {
		t.Fatalf("service = %#v, want stable service payload", got.Service)
	}
	if got.LatestEvent.ID != "svcevt-1" ||
		got.LatestEvent.Type != "replacement-blocked" ||
		got.LatestEvent.Phase != "blocked" ||
		got.LatestEvent.DiagnosticCode != "secret-projection-error" ||
		got.LatestEvent.ReplicaID != "alloc-1" {
		t.Fatalf("latest_event = %#v, want stable event payload", got.LatestEvent)
	}
	for _, unexpected := range []string{"SERVICE_STATUS", "SERVICE_EVENT_TYPE", "SERVICE_DIAGNOSTIC_CODE", "WORKLOAD_DIAGNOSTIC_CODE"} {
		if strings.Contains(b.String(), unexpected) {
			t.Fatalf("service get JSON leaked protobuf internals %q: %s", unexpected, b.String())
		}
	}
}

func TestPrintServiceDescribeJSONUsesNullLatestEvent(t *testing.T) {
	var b strings.Builder
	if err := PrintServiceDescribeJSON(&b, &servicev1.Service{ID: "svc-1"}, nil); err != nil {
		t.Fatal(err)
	}
	var got struct {
		LatestEvent json.RawMessage `json:"latest_event"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if string(got.LatestEvent) != "null" {
		t.Fatalf("latest_event = %s, want null", got.LatestEvent)
	}
}

func TestPrintServiceResponseJSONIncludesAdmissionDiagnosticFields(t *testing.T) {
	var b strings.Builder
	err := PrintServiceResponseJSON(&b, &servicev1.Service{
		ID:            "svc-quota",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Message:       "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Service struct {
			DiagnosticCode   string `json:"diagnostic_code"`
			AdmissionSummary string `json:"admission_summary"`
		} `json:"service"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Service.DiagnosticCode != "admission-blocked" || got.Service.AdmissionSummary != "namespace quota exceeded" {
		t.Fatalf("service JSON = %#v, want admission diagnostic fields", got.Service)
	}
}

func TestPrintServiceResponseJSONPrefersRolloutDiagnosticCode(t *testing.T) {
	var b strings.Builder
	err := PrintServiceResponseJSON(&b, &servicev1.Service{
		ID:            "svc-runtime",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		RolloutStatus: &servicev1.ServiceRolloutStatus{
			DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
		},
		Message: "runtime start failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Service struct {
			DiagnosticCode   string `json:"diagnostic_code"`
			AdmissionSummary string `json:"admission_summary"`
		} `json:"service"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Service.DiagnosticCode != "runtime-start-error" || got.Service.AdmissionSummary != "" {
		t.Fatalf("service JSON = %#v, want rollout diagnostic without admission summary", got.Service)
	}
}

func TestPrintServiceReplicaListJSONUsesStableStatusAndTimestamps(t *testing.T) {
	var b strings.Builder
	err := PrintServiceReplicaListJSON(&b, &servicev1.ListServiceReplicasResponse{
		Replicas: []*servicev1.ServiceReplica{{
			ID:        "alloc-1",
			ServiceID: "svc-1",
			Status:    commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			CreatedAt: timestamppb.New(time.Date(
				2026, time.April, 29, 12, 0, 0, 0, time.UTC,
			)),
			UpdatedAt: timestamppb.New(time.Date(
				2026, time.April, 29, 12, 1, 0, 0, time.UTC,
			)),
			ExitCode:      0,
			ExitCodeKnown: true,
			Ready:         true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Replicas []struct {
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			ExitCode  *int32 `json:"exit_code"`
		} `json:"replicas"`
	}
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got.Replicas[0].Status != "running" {
		t.Fatalf("status = %q, want running", got.Replicas[0].Status)
	}
	if got.Replicas[0].CreatedAt != "2026-04-29T12:00:00Z" || got.Replicas[0].UpdatedAt != "2026-04-29T12:01:00Z" {
		t.Fatalf("timestamps = %#v, want RFC3339 strings", got.Replicas[0])
	}
	if got.Replicas[0].ExitCode == nil || *got.Replicas[0].ExitCode != 0 {
		t.Fatalf("exit_code = %#v, want known zero", got.Replicas[0].ExitCode)
	}
	if strings.Contains(b.String(), `"seconds"`) || strings.Contains(b.String(), "ALLOCATION_STATUS") {
		t.Fatalf("replica JSON leaked protobuf internals: %s", b.String())
	}
}
