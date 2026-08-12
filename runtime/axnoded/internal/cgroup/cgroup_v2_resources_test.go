package cgroup

import (
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestCgroupV2SwapLimitConvertsOCISwapTotal(t *testing.T) {
	limit := int64(256 << 20)
	total := limit

	converted, err := cgroupV2SwapLimit(&specs.LinuxMemory{Limit: &limit, Swap: &total})
	if err != nil {
		t.Fatalf("cgroupV2SwapLimit() error = %v", err)
	}
	if converted == nil || *converted != 0 {
		t.Fatalf("memory.swap.max = %v, want 0", converted)
	}
}

func TestCgroupV2SwapLimitRejectsSwapTotalBelowMemory(t *testing.T) {
	limit := int64(256 << 20)
	total := limit - 1

	if _, err := cgroupV2SwapLimit(&specs.LinuxMemory{Limit: &limit, Swap: &total}); err == nil {
		t.Fatal("cgroupV2SwapLimit() accepted swap total below memory limit")
	}
}
