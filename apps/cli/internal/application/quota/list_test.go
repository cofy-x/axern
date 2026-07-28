package quota

import (
	"testing"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestPrepareListFiltersAndSortsPressure(t *testing.T) {
	quotas := []*quotav1.NamespaceQuota{
		{Namespace: "unlimited"},
		{Namespace: "medium", CpuMilliLimit: wrapperspb.Int64(1000), ReservedCpuMilli: 500},
		{Namespace: "hot", CpuMilliLimit: wrapperspb.Int64(1000), ReservedCpuMilli: 900},
		{Namespace: "memory-hot", MemoryBytesLimit: wrapperspb.Int64(1000), ReservedMemoryBytes: 950},
	}
	got, err := PrepareList(quotas, ListOptions{PressureOnly: true, Sort: "pressure"})
	if err != nil {
		t.Fatalf("PrepareList returned error: %v", err)
	}
	if len(got) != 2 || got[0].GetNamespace() != "memory-hot" || got[1].GetNamespace() != "hot" {
		t.Fatalf("filtered pressure quotas = %#v", got)
	}
}

func TestPrepareListConstrainedLimit(t *testing.T) {
	quotas := []*quotav1.NamespaceQuota{
		{Namespace: "unlimited"},
		{Namespace: "constrained-b", CpuMilliLimit: wrapperspb.Int64(1000)},
		{Namespace: "constrained-a", MemoryBytesLimit: wrapperspb.Int64(1 << 30)},
	}
	got, err := PrepareList(quotas, ListOptions{ConstrainedOnly: true, Sort: "namespace", Limit: 1})
	if err != nil {
		t.Fatalf("PrepareList returned error: %v", err)
	}
	if len(got) != 1 || got[0].GetNamespace() != "constrained-a" {
		t.Fatalf("filtered constrained quotas = %#v", got)
	}
}

func TestPrepareListZeroLimitDoesNotTruncate(t *testing.T) {
	quotas := []*quotav1.NamespaceQuota{
		{Namespace: "a", CpuMilliLimit: wrapperspb.Int64(1000)},
		{Namespace: "b", CpuMilliLimit: wrapperspb.Int64(1000)},
	}
	got, err := PrepareList(quotas, ListOptions{ConstrainedOnly: true, Sort: "pressure", Limit: 0})
	if err != nil {
		t.Fatalf("PrepareList returned error: %v", err)
	}
	if len(got) != len(quotas) {
		t.Fatalf("filtered quotas length = %d, want %d", len(got), len(quotas))
	}
}

func TestPrepareListRejectsInvalidSort(t *testing.T) {
	if _, err := PrepareList(nil, ListOptions{Sort: "surprise"}); err == nil {
		t.Fatal("PrepareList returned nil error, want invalid sort")
	}
}
