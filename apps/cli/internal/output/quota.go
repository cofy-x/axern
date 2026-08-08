package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func RenderNamespaceQuota(w io.Writer, quota *quotav1.NamespaceQuota) {
	if quota == nil {
		return
	}
	fmt.Fprintf(w, "Namespace: %s\n", quota.GetNamespace())
	fmt.Fprintf(w, "CPU Limit: %s\n", formatOptionalQuotaCPU(quota.GetCpuMilliLimit()))
	fmt.Fprintf(w, "CPU Reserved: %s\n", formatQuotaCPU(quota.GetReservedCpuMilli()))
	fmt.Fprintf(w, "CPU Available: %s\n", formatOptionalQuotaCPU(quota.GetAvailableCpuMilli()))
	fmt.Fprintf(w, "Memory Limit: %s\n", formatOptionalQuotaMemory(quota.GetMemoryBytesLimit()))
	fmt.Fprintf(w, "Memory Reserved: %s\n", formatQuotaMemory(quota.GetReservedMemoryBytes()))
	fmt.Fprintf(w, "Memory Available: %s\n", formatOptionalQuotaMemory(quota.GetAvailableMemoryBytes()))
	fmt.Fprintf(w, "Writable Layer Limit: %s\n", formatOptionalQuotaMemory(quota.GetWritableLayerBytesLimit()))
	fmt.Fprintf(w, "Writable Layer Reserved: %s\n", formatQuotaMemory(quota.GetReservedWritableLayerBytes()))
	fmt.Fprintf(w, "Writable Layer Available: %s\n", formatOptionalQuotaMemory(quota.GetAvailableWritableLayerBytes()))
}

func RenderNamespaceQuotaTable(w io.Writer, quotas []*quotav1.NamespaceQuota) {
	rows := make([][]string, 0, len(quotas))
	for _, quota := range quotas {
		if quota == nil {
			continue
		}
		rows = append(rows, []string{
			quota.GetNamespace(),
			formatQuotaCPUUsage(quota.GetReservedCpuMilli(), quota.GetCpuMilliLimit()),
			formatQuotaMemoryUsage(quota.GetReservedMemoryBytes(), quota.GetMemoryBytesLimit()),
			formatQuotaMemoryUsage(quota.GetReservedWritableLayerBytes(), quota.GetWritableLayerBytesLimit()),
		})
	}
	RenderTable(w, []string{"NAMESPACE", "CPU", "MEMORY", "WRITABLE LAYER"}, rows)
}

func RenderNamespaceQuotaDescribe(w io.Writer, quota *quotav1.NamespaceQuota, services []*servicev1.Service) {
	RenderNamespaceQuota(w, quota)
	fmt.Fprintln(w, "Admission Blocked Services:")
	if len(services) == 0 {
		fmt.Fprintln(w, "-")
		return
	}
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		rows = append(rows, []string{
			service.GetID(),
			ServiceStatusLabel(service.GetStatus()),
			workloaddiagnostic.AdmissionBlockedSummary(service.GetMessage()),
		})
	}
	RenderTable(w, []string{"SERVICE", "STATUS", "ADMISSION"}, rows)
}

func RenderNamespaceQuotaEventTable(w io.Writer, events []*quotav1.NamespaceQuotaEvent) {
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		rows = append(rows, []string{
			FormatProtoTimestamp(event.GetCreatedAt()),
			event.GetNamespace(),
			quotaEventWorkload(event),
			quotaEventReason(event.GetReason()),
			formatQuotaCPU(event.GetRequestedCpuMilli()),
			formatQuotaMemory(event.GetRequestedMemoryBytes()),
			formatQuotaMemory(event.GetRequestedWritableLayerBytes()),
		})
	}
	RenderTable(w, []string{"CREATED", "NAMESPACE", "WORKLOAD", "REASON", "CPU", "MEMORY", "WRITABLE LAYER"}, rows)
}

func formatOptionalQuotaCPU(value *wrapperspb.Int64Value) string {
	if value == nil {
		return "unlimited"
	}
	return formatQuotaCPU(value.GetValue())
}

func quotaEventWorkload(event *quotav1.NamespaceQuotaEvent) string {
	kind := strings.TrimPrefix(strings.ToLower(event.GetWorkloadType().String()), "namespace_quota_event_workload_type_")
	if kind == "unspecified" || kind == "" {
		kind = "-"
	}
	if event.GetWorkloadID() == "" {
		return kind
	}
	return kind + "/" + event.GetWorkloadID()
}

func quotaEventReason(reason quotav1.NamespaceQuotaEventReason) string {
	value := strings.TrimPrefix(strings.ToLower(reason.String()), "namespace_quota_event_reason_")
	if value == "unspecified" || value == "" {
		return "-"
	}
	return strings.ReplaceAll(value, "_", "-")
}

func formatQuotaCPU(cpuMilli int64) string {
	if cpuMilli < 1000 && cpuMilli > -1000 {
		return fmt.Sprintf("%dm", cpuMilli)
	}
	return strconv.FormatFloat(float64(cpuMilli)/1000, 'f', -1, 64) + " CPU"
}

func formatOptionalQuotaMemory(value *wrapperspb.Int64Value) string {
	if value == nil {
		return "unlimited"
	}
	return formatQuotaMemory(value.GetValue())
}

func formatQuotaCPUUsage(reserved int64, limit *wrapperspb.Int64Value) string {
	return formatQuotaUsage(formatQuotaCPU(reserved), formatOptionalQuotaTableLimit(formatQuotaCPU, limit), reserved, limit)
}

func formatQuotaMemoryUsage(reserved int64, limit *wrapperspb.Int64Value) string {
	return formatQuotaUsage(formatQuotaMemory(reserved), formatOptionalQuotaTableLimit(formatQuotaMemory, limit), reserved, limit)
}

func formatOptionalQuotaTableLimit(format func(int64) string, value *wrapperspb.Int64Value) string {
	if value == nil {
		return "-"
	}
	return format(value.GetValue())
}

func formatQuotaUsage(reservedText, limitText string, reserved int64, limit *wrapperspb.Int64Value) string {
	text := fmt.Sprintf("%s / %s", reservedText, limitText)
	if limit == nil {
		return text
	}
	if limit.GetValue() <= 0 || reserved <= 0 {
		return text + " (0%)"
	}
	percent := (reserved * 100) / limit.GetValue()
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%s (%d%%)", text, percent)
}

func formatQuotaMemory(bytes int64) string {
	type unit struct {
		suffix string
		size   int64
	}
	for _, candidate := range []unit{
		{suffix: "TiB", size: 1 << 40},
		{suffix: "GiB", size: 1 << 30},
		{suffix: "MiB", size: 1 << 20},
		{suffix: "KiB", size: 1 << 10},
	} {
		if bytes != 0 && bytes%candidate.size == 0 {
			return fmt.Sprintf("%d%s", bytes/candidate.size, candidate.suffix)
		}
	}
	return fmt.Sprintf("%dB", bytes)
}
