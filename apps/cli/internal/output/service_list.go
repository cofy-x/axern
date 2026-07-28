package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type ServiceListTableOptions struct {
	Wide       bool
	ShowLabels bool
}

func RenderServiceTable(w io.Writer, services []*servicev1.Service, opts ServiceListTableOptions) {
	headers := []string{"ID", "NAMESPACE", "STATUS", "READY", "DESIRED"}
	if opts.Wide {
		headers = append(headers, "UNHEALTHY", "AUTOSCALE", "ROLLOUT")
	}
	headers = append(headers, "AGE")
	if opts.ShowLabels {
		headers = append(headers, "LABELS")
	}
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		values := []string{
			service.GetID(),
			firstNonEmpty(service.GetNamespace(), "-"),
			ServiceStatusLabel(service.GetStatus()),
			fmt.Sprintf("%d/%d", service.GetReadyReplicas(), service.GetReplicas()),
			fmt.Sprintf("%d", service.GetReplicas()),
		}
		if opts.Wide {
			values = append(
				values,
				fmt.Sprintf("%d", service.GetUnhealthyReplicas()),
				serviceAutoscalingIndicator(service),
				serviceRolloutIndicator(service),
			)
		}
		values = append(values, formatServiceAge(service))
		if opts.ShowLabels {
			values = append(values, formatLabels(service.GetLabels()))
		}
		rows = append(rows, values)
	}
	RenderTable(w, headers, rows)
}

func serviceRolloutIndicator(service *servicev1.Service) string {
	rollout := service.GetRolloutStatus()
	if rollout == nil || !rollout.GetInProgress() {
		return "-"
	}
	switch rollout.GetPhase() {
	case servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY:
		return "waiting-ready"
	case servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED:
		return "blocked"
	default:
		return "in-progress"
	}
}

func serviceAutoscalingIndicator(service *servicev1.Service) string {
	if service.GetAutoscalingPolicy() == nil {
		return "-"
	}
	autoscaling := service.GetAutoscalingStatus()
	if autoscaling == nil || autoscaling.GetActiveScheduleName() == "" {
		return "idle"
	}
	return fmt.Sprintf("%s:%d", ShortMessage(autoscaling.GetActiveScheduleName(), 12), autoscaling.GetCurrentDesiredReplicas())
}

func formatServiceAge(service *servicev1.Service) string {
	if service == nil || service.GetCreatedAt() == nil {
		return "-"
	}
	return FormatRelativeAge(service.GetCreatedAt().AsTime(), time.Now().UTC())
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, labels[key]))
	}
	return strings.Join(parts, ",")
}
