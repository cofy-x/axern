package cgroup

import (
	"fmt"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// cgroupV2SwapLimit preserves the OCI memory.swap contract across cgroups
// library versions. OCI expresses swap as the total memory-plus-swap limit,
// while cgroup v2 memory.swap.max accepts the swap-only limit.
func cgroupV2SwapLimit(memory *specs.LinuxMemory) (*int64, error) {
	if memory == nil || memory.Swap == nil {
		return nil, nil
	}
	if memory.Limit == nil || *memory.Swap < 0 {
		return memory.Swap, nil
	}

	total := *memory.Swap
	limit := *memory.Limit
	if total >= 0 && total < limit {
		return nil, fmt.Errorf("OCI memory swap total %d is below memory limit %d", total, limit)
	}
	swapOnly := total - limit
	return &swapOnly, nil
}
