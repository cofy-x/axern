package nodeinventory

import (
	"strings"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func readySource(now time.Time) SourceStatus {
	return SourceStatus{
		Status:        StatusReady,
		LastSuccessAt: &now,
	}
}

func degradedSource(message string, now time.Time) SourceStatus {
	return SourceStatus{
		Status:        StatusDegraded,
		LastSuccessAt: &now,
		Error:         message,
	}
}

func errorSource(err error) SourceStatus {
	return SourceStatus{
		Status: StatusError,
		Error:  err.Error(),
	}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func cpuCommitmentMilli(spec *commonv1.ResourceSpec, res *runtimeapi.LinuxContainerResources) (int64, bool) {
	if cpu := spec.GetRequests().GetCpuMilli(); cpu > 0 {
		return cpu, true
	}
	if res == nil {
		return 0, false
	}
	if res.CpuQuota > 0 && res.CpuPeriod > 0 {
		return res.CpuQuota * 1000 / int64(res.CpuPeriod), true
	}
	if res.CpuShares > 0 {
		return int64(res.CpuShares * 1000 / 1024), true
	}
	return 0, false
}

func memoryCommitmentBytes(spec *commonv1.ResourceSpec, res *runtimeapi.LinuxContainerResources) (int64, bool) {
	if memory := spec.GetRequests().GetMemoryBytes(); memory > 0 {
		return memory, true
	}
	if res == nil || res.MemoryLimitInBytes <= 0 {
		return 0, false
	}
	return res.MemoryLimitInBytes, true
}

func cpuUsedMilli(prev, current cpuUsageSample) (int64, bool) {
	if !prev.CollectedAt.Before(current.CollectedAt) || current.UsageNs <= prev.UsageNs {
		return 0, false
	}
	elapsedNs := current.CollectedAt.Sub(prev.CollectedAt).Nanoseconds()
	if elapsedNs <= 0 {
		return 0, false
	}
	return int64(current.UsageNs-prev.UsageNs) * 1000 / elapsedNs, true
}
