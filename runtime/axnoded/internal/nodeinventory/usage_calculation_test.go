package nodeinventory

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestCPUCommitmentMilli(t *testing.T) {
	tests := []struct {
		name string
		spec *commonv1.ResourceSpec
		want int64
		ok   bool
	}{
		{name: "request", spec: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{CpuMilli: 500}}, want: 500, ok: true},
		{name: "limit fallback", spec: &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{CpuMilli: 2000}}, want: 2000, ok: true},
		{name: "unbounded", spec: &commonv1.ResourceSpec{}, want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cpuCommitmentMilli(tt.spec)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("cpuCommitmentMilli() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMemoryCommitmentBytes(t *testing.T) {
	got, ok := memoryCommitmentBytes(&commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{MemoryBytes: 512}})
	if !ok || got != 512 {
		t.Fatalf("memoryCommitmentBytes() = (%d, %v), want (512, true)", got, ok)
	}

	got, ok = memoryCommitmentBytes(&commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 256}})
	if !ok || got != 256 {
		t.Fatalf("memoryCommitmentBytes(request) = (%d, %v), want (256, true)", got, ok)
	}

	if _, ok := memoryCommitmentBytes(nil); ok {
		t.Fatal("expected zero memory limit to be unbounded")
	}
}

func TestCPUUsedMilli(t *testing.T) {
	prev := cpuUsageSample{UsageNs: 100_000_000, CollectedAt: time.Unix(0, 0)}
	current := cpuUsageSample{UsageNs: 300_000_000, CollectedAt: time.Unix(0, int64(time.Second))}
	got, ok := cpuUsedMilli(prev, current)
	if !ok || got != 200 {
		t.Fatalf("cpuUsedMilli() = (%d, %v), want (200, true)", got, ok)
	}
}

func TestBPFNetComponentInventory(t *testing.T) {
	component := bpfnetComponentInventory(bpfnet.Status{
		State: bpfnet.DataplaneState{
			Mode:    "tc",
			TCReady: false,
		},
	})

	if component.Ready {
		t.Fatal("expected non-ready bpfnet component")
	}
}
