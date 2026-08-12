//go:build !linux

package hostlinux

import "fmt"

func InspectEnforcedNodeMemoryBudget(int64, int64, int64, string) (NodeMemoryBudgetSample, error) {
	return NodeMemoryBudgetSample{}, fmt.Errorf("node memory budget requires Linux")
}

func InspectDevelopmentNodeMemoryBudget(int64, int64) (NodeMemoryBudgetSample, error) {
	return NodeMemoryBudgetSample{}, fmt.Errorf("node memory budget requires Linux")
}
