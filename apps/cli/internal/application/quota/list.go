package quota

import (
	"fmt"
	"sort"
	"strings"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const pressureThresholdPercent int64 = 80

type ListOptions struct {
	ConstrainedOnly bool
	PressureOnly    bool
	Sort            string
	Limit           int
}

func PrepareList(quotas []*quotav1.NamespaceQuota, options ListOptions) ([]*quotav1.NamespaceQuota, error) {
	sortMode := strings.ToLower(strings.TrimSpace(options.Sort))
	if sortMode == "" {
		sortMode = "namespace"
	}
	switch sortMode {
	case "namespace", "pressure", "updated":
	default:
		return nil, fmt.Errorf("--sort must be namespace, pressure, or updated")
	}
	out := make([]*quotav1.NamespaceQuota, 0, len(quotas))
	for _, quota := range quotas {
		if quota == nil {
			continue
		}
		if options.ConstrainedOnly && !quotaConstrained(quota) {
			continue
		}
		if options.PressureOnly && quotaPressurePercent(quota) < pressureThresholdPercent {
			continue
		}
		out = append(out, quota)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch sortMode {
		case "pressure":
			if quotaPressurePercent(a) != quotaPressurePercent(b) {
				return quotaPressurePercent(a) > quotaPressurePercent(b)
			}
			if quotaConstrained(a) != quotaConstrained(b) {
				return quotaConstrained(a)
			}
			if quotaReserved(a) != quotaReserved(b) {
				return quotaReserved(a)
			}
		case "updated":
			if quotaUpdatedUnix(a) != quotaUpdatedUnix(b) {
				return quotaUpdatedUnix(a) > quotaUpdatedUnix(b)
			}
		}
		return a.GetNamespace() < b.GetNamespace()
	})
	if options.Limit > 0 && len(out) > options.Limit {
		out = out[:options.Limit]
	}
	return out, nil
}

func quotaUpdatedUnix(quota *quotav1.NamespaceQuota) int64 {
	if quota.GetUpdatedAt() == nil || !quota.GetUpdatedAt().IsValid() {
		return 0
	}
	return quota.GetUpdatedAt().AsTime().UnixNano()
}

func quotaConstrained(quota *quotav1.NamespaceQuota) bool {
	return quota.GetCpuMilliLimit() != nil || quota.GetMemoryBytesLimit() != nil
}

func quotaReserved(quota *quotav1.NamespaceQuota) bool {
	return quota.GetReservedCpuMilli() > 0 || quota.GetReservedMemoryBytes() > 0
}

func quotaPressurePercent(quota *quotav1.NamespaceQuota) int64 {
	return max(
		quotaUsagePercent(quota.GetReservedCpuMilli(), quota.GetCpuMilliLimit()),
		quotaUsagePercent(quota.GetReservedMemoryBytes(), quota.GetMemoryBytesLimit()),
	)
}

func quotaUsagePercent(reserved int64, limit *wrapperspb.Int64Value) int64 {
	if limit == nil || limit.GetValue() <= 0 || reserved <= 0 {
		return 0
	}
	percent := (reserved * 100) / limit.GetValue()
	if percent > 100 {
		return 100
	}
	return percent
}
