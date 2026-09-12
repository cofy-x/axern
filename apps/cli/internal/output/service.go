package output

import (
	"fmt"
	"io"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func RenderService(w io.Writer, service *servicev1.Service) {
	renderServiceDetails(w, service, nil, false)
}

func RenderServiceDescribe(w io.Writer, service *servicev1.Service, latestEvent *servicev1.ServiceEvent) {
	renderServiceDetails(w, service, latestEvent, true)
}

func RenderServiceDeletionResult(w io.Writer, service *servicev1.Service) {
	if service == nil {
		return
	}
	phase := service.GetDeletionStatus().GetPhase()
	if phase == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		fmt.Fprintf(w, "Service deleted: %s\n", service.GetID())
		return
	}
	fmt.Fprintf(w, "Service deletion requested: %s (phase=%s)\n", service.GetID(), ServiceDeletionPhaseLabel(phase))
}

func renderServiceDetails(w io.Writer, service *servicev1.Service, latestEvent *servicev1.ServiceEvent, includeDescribeFields bool) {
	if service == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", service.GetID())
	fmt.Fprintf(w, "Namespace: %s\n", service.GetNamespace())
	fmt.Fprintf(w, "Environment ID: %s\n", service.GetEnvironmentID())
	fmt.Fprintf(w, "Status: %s\n", ServiceStatusLabel(service.GetStatus()))
	if deletion := service.GetDeletionStatus(); deletion != nil {
		fmt.Fprintf(
			w,
			"Deletion: phase=%s\n",
			ServiceDeletionPhaseLabel(deletion.GetPhase()),
		)
		if completedAt := FormatProtoTimestamp(deletion.GetCompletedAt()); completedAt != "" {
			fmt.Fprintf(w, "Deletion Completed At: %s\n", completedAt)
		}
		if message := deletion.GetMessage(); message != "" {
			fmt.Fprintf(w, "Deletion Message: %s\n", message)
		}
	}
	fmt.Fprintf(w, "Replicas: desired=%d ready=%d unhealthy=%d\n", service.GetReplicas(), service.GetReadyReplicas(), service.GetUnhealthyReplicas())
	if includeDescribeFields {
		fmt.Fprintf(w, "Labels: %s\n", formatLabels(service.GetLabels()))
		fmt.Fprintf(w, "Version: %d\n", service.GetVersion())
		if created := FormatProtoTimestamp(service.GetCreatedAt()); created != "" {
			fmt.Fprintf(w, "Created At: %s\n", created)
		}
		if updated := FormatProtoTimestamp(service.GetUpdatedAt()); updated != "" {
			fmt.Fprintf(w, "Updated At: %s\n", updated)
		}
	}
	if mounts := service.GetConfig().GetImageMounts(); len(mounts) > 0 {
		fmt.Fprintf(w, "Image Mounts: %s\n", formatImageMounts(mounts))
	}
	if probe := service.GetReadinessProbe(); serviceProbeConfigured(probe) {
		fmt.Fprintf(w, "Readiness Probe: %s\n", formatServiceProbe(probe))
	}
	if probe := service.GetLivenessProbe(); serviceProbeConfigured(probe) {
		fmt.Fprintf(w, "Liveness Probe: %s\n", formatServiceProbe(probe))
	}
	if policy := service.GetRolloutPolicy(); policy != nil {
		fmt.Fprintf(w, "Rollout Policy: max_surge=%d max_unavailable=%d\n", policy.GetMaxSurge(), policy.GetMaxUnavailable())
	}
	if rollout := service.GetRolloutStatus(); rollout != nil {
		fmt.Fprintf(
			w,
			"Rollout: in_progress=%t phase=%s current=%d updated_ready=%d outdated=%d\n",
			rollout.GetInProgress(),
			ServiceRolloutPhaseLabel(rollout.GetPhase()),
			rollout.GetCurrentReplicas(),
			rollout.GetUpdatedReadyReplicas(),
			rollout.GetOutdatedReplicas(),
		)
		if rollout.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			fmt.Fprintf(w, "Rollout Diagnostic: %s\n", WorkloadDiagnosticCodeLabel(rollout.GetDiagnosticCode()))
		}
		if detail := rollout.GetDiagnosticMessage(); detail != "" {
			fmt.Fprintf(w, "Rollout Detail: %s\n", detail)
		}
	}
	if latestEvent != nil {
		RenderServiceLatestEvent(w, latestEvent)
	}
	if message := service.GetMessage(); message != "" {
		code := service.GetDiagnosticCode()
		if code == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED && workloaddiagnostic.AdmissionBlocked(message) {
			code = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED
		}
		if code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			fmt.Fprintf(w, "Diagnostic: %s\n", WorkloadDiagnosticCodeLabel(code))
		}
		if admission := workloaddiagnostic.AdmissionBlockedSummary(message); admission != "" {
			fmt.Fprintf(w, "Admission: %s\n", admission)
		}
		fmt.Fprintf(w, "Message: %s\n", message)
	}
}

func serviceProbeConfigured(probe *servicev1.ServiceProbe) bool {
	return newServiceProbeJSON(probe) != nil
}
