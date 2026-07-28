package output

import (
	"bytes"
	"strings"
	"testing"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRenderNamespaceQuotaUsesFriendlyUnits(t *testing.T) {
	quota := &quotav1.NamespaceQuota{
		Namespace:            "team-a",
		CpuMilliLimit:        wrapperspb.Int64(1500),
		ReservedCpuMilli:     500,
		AvailableCpuMilli:    wrapperspb.Int64(1000),
		MemoryBytesLimit:     wrapperspb.Int64(8 << 30),
		ReservedMemoryBytes:  512 << 20,
		AvailableMemoryBytes: wrapperspb.Int64(7680 << 20),
	}
	var out bytes.Buffer
	RenderNamespaceQuota(&out, quota)
	for _, want := range []string{
		"CPU Limit: 1.5 CPU",
		"CPU Reserved: 500m",
		"CPU Available: 1 CPU",
		"Memory Limit: 8GiB",
		"Memory Reserved: 512MiB",
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
		ReservedCpuMilli:     750,
		MemoryBytesLimit:     wrapperspb.Int64(1 << 30),
		ReservedMemoryBytes:  128 << 20,
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
		Namespace:           "default",
		ReservedCpuMilli:    500,
		ReservedMemoryBytes: 4 << 30,
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

func TestRenderNamespaceQuotaDescribeShowsAdmissionBlockedServices(t *testing.T) {
	quota := &quotav1.NamespaceQuota{Namespace: "team-a"}
	service := &servicev1.Service{
		ID:      "svc-a",
		Status:  servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Message: "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
	}
	var out bytes.Buffer
	RenderNamespaceQuotaDescribe(&out, quota, []*servicev1.Service{service})
	for _, want := range []string{"Admission Blocked Services", "svc-a", "degraded", "namespace quota exceeded"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("describe output missing %q:\n%s", want, out.String())
		}
	}
}
