package output

import (
	"bytes"
	"strings"
	"testing"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRenderNamespaceQuotaUsesFriendlyUnits(t *testing.T) {
	quota := &quotav1.NamespaceQuota{
		Namespace:            "team-a",
		CpuMilliLimit:        wrapperspb.Int64(1500),
		UsedCpuMilli:         500,
		AvailableCpuMilli:    wrapperspb.Int64(1000),
		MemoryBytesLimit:     wrapperspb.Int64(8 << 30),
		UsedMemoryBytes:      512 << 20,
		AvailableMemoryBytes: wrapperspb.Int64(7680 << 20),
	}
	var out bytes.Buffer
	RenderNamespaceQuota(&out, quota)
	for _, want := range []string{
		"CPU Limit: 1.5 CPU",
		"CPU Used: 500m",
		"CPU Available: 1 CPU",
		"Memory Limit: 8GiB",
		"Memory Used: 512MiB",
		"Memory Available: 7680MiB",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRenderNamespaceQuotaTableUsesFriendlyUnits(t *testing.T) {
	quota := &quotav1.NamespaceQuota{
		Namespace:            "team-a",
		CpuMilliLimit:        wrapperspb.Int64(2000),
		UsedCpuMilli:         750,
		MemoryBytesLimit:     wrapperspb.Int64(1 << 30),
		UsedMemoryBytes:      128 << 20,
		AvailableMemoryBytes: wrapperspb.Int64(896 << 20),
	}
	var out bytes.Buffer
	RenderNamespaceQuotaTable(&out, []*quotav1.NamespaceQuota{quota})
	for _, want := range []string{"NAMESPACE", "CPU", "MEMORY", "750m / 2 CPU (37%)", "128MiB / 1GiB (12%)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table output missing %q:\n%s", want, out.String())
		}
	}
	for _, unwanted := range []string{"CPU AVAILABLE", "MEMORY AVAILABLE", "896MiB"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("table output contains verbose field %q:\n%s", unwanted, out.String())
		}
	}
}

func TestRenderNamespaceQuotaTableUsesCompactUnlimitedMarker(t *testing.T) {
	quota := &quotav1.NamespaceQuota{
		Namespace:       "default",
		UsedCpuMilli:    500,
		UsedMemoryBytes: 4 << 30,
	}
	var out bytes.Buffer
	RenderNamespaceQuotaTable(&out, []*quotav1.NamespaceQuota{quota})
	for _, want := range []string{"500m / -", "4GiB / -"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("table output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "unlimited") {
		t.Fatalf("table output should use compact unlimited marker:\n%s", out.String())
	}
}

func TestRenderNamespaceQuotaEventUsesExistingEnvironmentIdentity(t *testing.T) {
	event := &quotav1.NamespaceQuotaEvent{
		ID:            "quotaevt-1",
		Namespace:     "team-a",
		Type:          quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED,
		EnvironmentID: "env-real",
		Reason:        quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU,
		CreatedAt:     timestamppb.Now(),
	}
	var out bytes.Buffer
	RenderNamespaceQuotaEventTable(&out, []*quotav1.NamespaceQuotaEvent{event})
	for _, want := range []string{"ENVIRONMENT", "env-real", "insufficient-cpu"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("event output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "RUN") {
		t.Fatalf("event output contains removed pseudo-Run identity:\n%s", out.String())
	}
}
