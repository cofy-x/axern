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
	tests := []struct {
		name    string
		usageNs uint64
		elapsed time.Duration
		want    int64
		ok      bool
	}{
		{name: "active", usageNs: 300_000_000, elapsed: time.Second, want: 200, ok: true},
		{name: "idle", usageNs: prev.UsageNs, elapsed: time.Second, ok: true},
		{name: "counter reset", usageNs: prev.UsageNs - 1, elapsed: time.Second},
		{name: "same time", usageNs: 300_000_000},
		{name: "idle same time", usageNs: prev.UsageNs},
		{name: "time reversed", usageNs: 300_000_000, elapsed: -time.Second},
		{name: "idle time reversed", usageNs: prev.UsageNs, elapsed: -time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := cpuUsageSample{UsageNs: tt.usageNs, CollectedAt: prev.CollectedAt.Add(tt.elapsed)}
			got, ok := cpuUsedMilli(prev, current)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("cpuUsedMilli() = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
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
