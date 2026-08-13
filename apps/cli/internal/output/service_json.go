package output

import (
	"io"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type ServiceListJSON struct {
	Services   []*ServiceJSON `json:"services"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type ServiceResponseJSON struct {
	Service *ServiceJSON `json:"service"`
}

type ServiceDescribeJSON struct {
	Service     *ServiceJSON      `json:"service"`
	LatestEvent *ServiceEventJSON `json:"latest_event"`
}

type ServiceJSON struct {
	ID                string                        `json:"id"`
	Namespace         string                        `json:"namespace"`
	EnvironmentID     string                        `json:"environment_id"`
	Replicas          int32                         `json:"replicas"`
	ReadyReplicas     int32                         `json:"ready_replicas"`
	UnhealthyReplicas int32                         `json:"unhealthy_replicas"`
	RolloutPolicy     *ServiceRolloutPolicyJSON     `json:"rollout_policy,omitempty"`
	RolloutStatus     *ServiceRolloutStatusJSON     `json:"rollout_status,omitempty"`
	Status            string                        `json:"status"`
	DeletionStatus    *ServiceDeletionStatusJSON    `json:"deletion_status,omitempty"`
	Config            *ExecutionConfigJSON          `json:"config,omitempty"`
	AllocationIDs     []string                      `json:"allocation_ids,omitempty"`
	Labels            map[string]string             `json:"labels,omitempty"`
	Version           int64                         `json:"version"`
	CreatedAt         string                        `json:"created_at,omitempty"`
	UpdatedAt         string                        `json:"updated_at,omitempty"`
	DiagnosticCode    string                        `json:"diagnostic_code,omitempty"`
	AdmissionSummary  string                        `json:"admission_summary,omitempty"`
	Message           string                        `json:"message,omitempty"`
	ReadinessProbe    *ServiceProbeJSON             `json:"readiness_probe"`
	LivenessProbe     *ServiceProbeJSON             `json:"liveness_probe"`
	AutoscalingPolicy *ServiceAutoscalingPolicyJSON `json:"autoscaling_policy,omitempty"`
	AutoscalingStatus *ServiceAutoscalingStatusJSON `json:"autoscaling_status,omitempty"`
}

type ServiceDeletionStatusJSON struct {
	Phase             string   `json:"phase"`
	VolumeDisposition string   `json:"volume_disposition"`
	ClaimIDs          []string `json:"claim_ids,omitempty"`
	Message           string   `json:"message,omitempty"`
	CompletedAt       string   `json:"completed_at,omitempty"`
}

type ServiceRolloutPolicyJSON struct {
	MaxSurge       int32 `json:"max_surge"`
	MaxUnavailable int32 `json:"max_unavailable"`
}

type ServiceProbeJSON struct {
	Type             string `json:"type"`
	Port             int32  `json:"port"`
	Path             string `json:"path,omitempty"`
	Scheme           string `json:"scheme,omitempty"`
	InitialDelay     string `json:"initial_delay"`
	Period           string `json:"period"`
	Timeout          string `json:"timeout"`
	SuccessThreshold int32  `json:"success_threshold"`
	FailureThreshold int32  `json:"failure_threshold"`
}

type ServiceRolloutStatusJSON struct {
	InProgress           bool   `json:"in_progress"`
	CurrentReplicas      int32  `json:"current_replicas"`
	UpdatedReadyReplicas int32  `json:"updated_ready_replicas"`
	OutdatedReplicas     int32  `json:"outdated_replicas"`
	Phase                string `json:"phase"`
	DiagnosticCode       string `json:"diagnostic_code"`
	DiagnosticMessage    string `json:"diagnostic_message,omitempty"`
}

type ServiceAutoscalingStatusJSON struct {
	CurrentDesiredReplicas int32  `json:"current_desired_replicas"`
	EffectiveMinReplicas   int32  `json:"effective_min_replicas"`
	EffectiveMaxReplicas   int32  `json:"effective_max_replicas"`
	ActiveScheduleName     string `json:"active_schedule_name,omitempty"`
	ActiveScheduleReplicas int32  `json:"active_schedule_replicas,omitempty"`
	LastEvaluatedAt        string `json:"last_evaluated_at,omitempty"`
	LastAction             string `json:"last_action"`
	Message                string `json:"message,omitempty"`
}

type ServiceAutoscalingPolicyJSON struct {
	MinReplicas int32                             `json:"min_replicas"`
	MaxReplicas int32                             `json:"max_replicas"`
	Schedules   []*ServiceAutoscalingScheduleJSON `json:"schedules,omitempty"`
}

type ServiceAutoscalingScheduleJSON struct {
	Name     string `json:"name"`
	CronUTC  string `json:"cron_utc"`
	Replicas int32  `json:"replicas"`
}

func PrintServiceListJSON(w io.Writer, resp *servicev1.ListServicesResponse) error {
	out := ServiceListJSON{}
	if resp != nil {
		out.NextCursor = resp.GetNextCursor()
		out.Services = make([]*ServiceJSON, 0, len(resp.GetServices()))
		for _, service := range resp.GetServices() {
			out.Services = append(out.Services, NewServiceJSON(service))
		}
	}
	return PrintJSON(w, out)
}

func PrintServiceResponseJSON(w io.Writer, service *servicev1.Service) error {
	return PrintJSON(w, ServiceResponseJSON{Service: NewServiceJSON(service)})
}

func PrintServiceDescribeJSON(w io.Writer, service *servicev1.Service, latestEvent *servicev1.ServiceEvent) error {
	return PrintJSON(w, ServiceDescribeJSON{
		Service:     NewServiceJSON(service),
		LatestEvent: NewServiceEventJSON(latestEvent),
	})
}

func NewServiceJSON(service *servicev1.Service) *ServiceJSON {
	if service == nil {
		return nil
	}
	return &ServiceJSON{
		ID:                service.GetID(),
		Namespace:         service.GetNamespace(),
		EnvironmentID:     service.GetEnvironmentID(),
		Replicas:          service.GetReplicas(),
		ReadyReplicas:     service.GetReadyReplicas(),
		UnhealthyReplicas: service.GetUnhealthyReplicas(),
		RolloutPolicy:     newServiceRolloutPolicyJSON(service.GetRolloutPolicy()),
		RolloutStatus:     newServiceRolloutStatusJSON(service.GetRolloutStatus()),
		Status:            ServiceStatusLabel(service.GetStatus()),
		DeletionStatus:    newServiceDeletionStatusJSON(service.GetDeletionStatus()),
		Config:            NewExecutionConfigJSON(service.GetConfig()),
		AllocationIDs:     append([]string(nil), service.GetAllocationIds()...),
		Labels:            cloneStringMap(service.GetLabels()),
		Version:           service.GetVersion(),
		CreatedAt:         FormatProtoTimestamp(service.GetCreatedAt()),
		UpdatedAt:         FormatProtoTimestamp(service.GetUpdatedAt()),
		DiagnosticCode:    serviceWorkloadDiagnosticCodeJSON(service),
		AdmissionSummary:  workloaddiagnostic.AdmissionBlockedSummary(service.GetMessage()),
		Message:           service.GetMessage(),
		ReadinessProbe:    newServiceProbeJSON(service.GetReadinessProbe()),
		LivenessProbe:     newServiceProbeJSON(service.GetLivenessProbe()),
		AutoscalingPolicy: newServiceAutoscalingPolicyJSON(service.GetAutoscalingPolicy()),
		AutoscalingStatus: newServiceAutoscalingStatusJSON(service.GetAutoscalingStatus()),
	}
}

func newServiceDeletionStatusJSON(status *servicev1.ServiceDeletionStatus) *ServiceDeletionStatusJSON {
	if status == nil {
		return nil
	}
	return &ServiceDeletionStatusJSON{
		Phase:             ServiceDeletionPhaseLabel(status.GetPhase()),
		VolumeDisposition: ServiceVolumeDispositionLabel(status.GetVolumeDisposition()),
		ClaimIDs:          append([]string(nil), status.GetClaimIds()...),
		Message:           status.GetMessage(),
		CompletedAt:       FormatProtoTimestamp(status.GetCompletedAt()),
	}
}

func serviceWorkloadDiagnosticCodeJSON(service *servicev1.Service) string {
	if service == nil {
		return ""
	}
	if code := service.GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return WorkloadDiagnosticCodeLabel(code)
	}
	if code := service.GetRolloutStatus().GetDiagnosticCode(); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return WorkloadDiagnosticCodeLabel(code)
	}
	return workloaddiagnostic.DiagnosticCode(service.GetMessage())
}

func newServiceProbeJSON(probe *servicev1.ServiceProbe) *ServiceProbeJSON {
	if probe == nil {
		return nil
	}
	out := &ServiceProbeJSON{
		InitialDelay:     formatProbeDuration(probe.GetInitialDelay()),
		Period:           formatProbeDuration(probe.GetPeriod()),
		Timeout:          formatProbeDuration(probe.GetTimeout()),
		SuccessThreshold: probe.GetSuccessThreshold(),
		FailureThreshold: probe.GetFailureThreshold(),
	}
	switch typed := probe.GetAction().(type) {
	case *servicev1.ServiceProbe_Http:
		if typed.Http == nil {
			return nil
		}
		out.Type = "http"
		out.Port = typed.Http.GetPort()
		out.Path = typed.Http.GetPath()
		out.Scheme = serviceHTTPProbeSchemeLabel(typed.Http.GetScheme())
	case *servicev1.ServiceProbe_Tcp:
		if typed.Tcp == nil {
			return nil
		}
		out.Type = "tcp"
		out.Port = typed.Tcp.GetPort()
	default:
		return nil
	}
	return out
}

func newServiceRolloutStatusJSON(status *servicev1.ServiceRolloutStatus) *ServiceRolloutStatusJSON {
	if status == nil {
		return nil
	}
	return &ServiceRolloutStatusJSON{
		InProgress:           status.GetInProgress(),
		CurrentReplicas:      status.GetCurrentReplicas(),
		UpdatedReadyReplicas: status.GetUpdatedReadyReplicas(),
		OutdatedReplicas:     status.GetOutdatedReplicas(),
		Phase:                ServiceRolloutPhaseLabel(status.GetPhase()),
		DiagnosticCode:       WorkloadDiagnosticCodeLabel(status.GetDiagnosticCode()),
		DiagnosticMessage:    status.GetDiagnosticMessage(),
	}
}

func newServiceRolloutPolicyJSON(policy *servicev1.ServiceRolloutPolicy) *ServiceRolloutPolicyJSON {
	if policy == nil {
		return nil
	}
	return &ServiceRolloutPolicyJSON{
		MaxSurge:       policy.GetMaxSurge(),
		MaxUnavailable: policy.GetMaxUnavailable(),
	}
}

func newServiceAutoscalingPolicyJSON(policy *servicev1.ServiceAutoscalingPolicy) *ServiceAutoscalingPolicyJSON {
	if policy == nil {
		return nil
	}
	if policy.GetMinReplicas() == 0 && policy.GetMaxReplicas() == 0 && len(policy.GetSchedules()) == 0 {
		return nil
	}
	out := &ServiceAutoscalingPolicyJSON{
		MinReplicas: policy.GetMinReplicas(),
		MaxReplicas: policy.GetMaxReplicas(),
		Schedules:   make([]*ServiceAutoscalingScheduleJSON, 0, len(policy.GetSchedules())),
	}
	for _, schedule := range policy.GetSchedules() {
		if schedule == nil {
			continue
		}
		out.Schedules = append(out.Schedules, &ServiceAutoscalingScheduleJSON{
			Name:     schedule.GetName(),
			CronUTC:  schedule.GetCronUtc(),
			Replicas: schedule.GetReplicas(),
		})
	}
	if len(out.Schedules) == 0 {
		out.Schedules = nil
	}
	return out
}

func newServiceAutoscalingStatusJSON(status *servicev1.ServiceAutoscalingStatus) *ServiceAutoscalingStatusJSON {
	if status == nil {
		return nil
	}
	return &ServiceAutoscalingStatusJSON{
		CurrentDesiredReplicas: status.GetCurrentDesiredReplicas(),
		EffectiveMinReplicas:   status.GetEffectiveMinReplicas(),
		EffectiveMaxReplicas:   status.GetEffectiveMaxReplicas(),
		ActiveScheduleName:     status.GetActiveScheduleName(),
		ActiveScheduleReplicas: status.GetActiveScheduleReplicas(),
		LastEvaluatedAt:        FormatProtoTimestamp(status.GetLastEvaluatedAt()),
		LastAction:             ServiceAutoscalingActionLabel(status.GetLastAction()),
		Message:                status.GetMessage(),
	}
}

func serviceHTTPProbeSchemeLabel(scheme servicev1.HttpProbeScheme) string {
	switch scheme {
	case servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTPS:
		return "https"
	default:
		return "http"
	}
}
