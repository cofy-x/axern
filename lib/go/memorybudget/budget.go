package memorybudget

import (
	"fmt"
	"strings"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

const (
	maxCapacityIdentityBytes = 1024
	// SampleFreshness is intentionally aligned with the 5 second node memory
	// sampler: one delayed report is tolerated, three missed samples are not.
	SampleFreshness = 15 * time.Second
	maxFutureSkew   = time.Minute
)

// Published reports whether a summary carries a node memory-capacity
// observation. Enforcement mode is explicit; disabled development nodes still
// publish scheduling capacity but cannot advertise hard-limit capabilities.
func Published(budget *nodev1.NodeMemoryBudget) bool {
	if budget == nil {
		return false
	}
	return budget.GetSourceAllocatableBytes() != 0 || budget.GetSystemReserveBytes() != 0 ||
		strings.TrimSpace(budget.GetCapacityIdentity()) != "" || budget.GetSampledAt() != nil ||
		budget.GetMode() != nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_UNSPECIFIED
}

// Validate checks the canonical node memory scheduling boundary independently
// of placement policy. The same function is used when selecting a node and
// when sealing admission evidence so malformed summaries cannot cross the
// optimistic/transactional boundary.
func Validate(budget *nodev1.NodeMemoryBudget) error {
	if budget == nil {
		return fmt.Errorf("node memory budget is required")
	}
	if budget.GetSourceAllocatableBytes() <= 0 {
		return fmt.Errorf("source allocatable must be positive")
	}
	switch budget.GetMode() {
	case nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_CGROUP_V2:
		if budget.GetSystemReserveBytes() <= 0 {
			return fmt.Errorf("cgroup v2 memory budget requires a positive system reserve")
		}
	case nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_DISABLED_DEV:
		if budget.GetSystemReserveBytes() != 0 {
			return fmt.Errorf("disabled_dev memory budget requires zero system reserve")
		}
		if budget.GetDelegatedRootLimitFinite() || budget.GetDelegatedRootLimitBytes() != 0 {
			return fmt.Errorf("disabled_dev memory budget cannot claim a delegated root limit")
		}
		if budget.GetSystemReserveExhausted() {
			return fmt.Errorf("disabled_dev memory budget cannot claim exhausted system reserve")
		}
	default:
		return fmt.Errorf("memory budget mode is required")
	}
	raw := budget.GetSourceAllocatableBytes()
	if budget.GetDelegatedRootLimitFinite() {
		if budget.GetDelegatedRootLimitBytes() <= 0 {
			return fmt.Errorf("finite delegated root limit must be positive")
		}
		raw = min(raw, budget.GetDelegatedRootLimitBytes())
	} else if budget.GetDelegatedRootLimitBytes() != 0 {
		return fmt.Errorf("unbounded delegated root cannot carry a finite limit")
	}
	if budget.GetSystemReserveBytes() >= raw {
		return fmt.Errorf("system reserve leaves no sandbox memory capacity")
	}
	identity := strings.TrimSpace(budget.GetCapacityIdentity())
	if identity == "" || len(identity) > maxCapacityIdentityBytes {
		return fmt.Errorf("capacity identity is missing or exceeds %d bytes", maxCapacityIdentityBytes)
	}
	if budget.GetSampledAt() == nil {
		return fmt.Errorf("memory budget sample time is required")
	}
	if err := budget.GetSampledAt().CheckValid(); err != nil {
		return fmt.Errorf("invalid memory budget sample time: %w", err)
	}
	return nil
}

// EffectiveAllocatable derives the sole scheduling value published on the
// enclosing NodeSummary from the observed memory boundary.
func EffectiveAllocatable(budget *nodev1.NodeMemoryBudget) int64 {
	if budget == nil {
		return 0
	}
	raw := budget.GetSourceAllocatableBytes()
	if budget.GetDelegatedRootLimitFinite() {
		raw = min(raw, budget.GetDelegatedRootLimitBytes())
	}
	return raw - budget.GetSystemReserveBytes()
}

// ValidateAt adds the scheduling freshness boundary to the structural
// contract. A fresh NodeSummary must never make an old memory-domain sample
// eligible again.
func ValidateAt(budget *nodev1.NodeMemoryBudget, now time.Time) error {
	if err := Validate(budget); err != nil {
		return err
	}
	sampledAt := budget.GetSampledAt().AsTime()
	if sampledAt.After(now.Add(maxFutureSkew)) {
		return fmt.Errorf("memory budget sample is in the future")
	}
	if now.Sub(sampledAt) > SampleFreshness {
		return fmt.Errorf("memory budget sample is stale")
	}
	return nil
}

// ValidateSummary binds the independently sampled capacity budget to the node
// resource surface that placement consumes.
func ValidateSummary(summary *nodev1.NodeSummary, now time.Time) error {
	if summary == nil {
		return fmt.Errorf("node summary is required")
	}
	budget := summary.GetMemoryBudget()
	if err := ValidateAt(budget, now); err != nil {
		return err
	}
	if summary.GetCapacity() == nil || summary.GetCapacity().GetMemoryBytes() <= 0 {
		return fmt.Errorf("capacity.memory_bytes must be positive")
	}
	if budget.GetSourceAllocatableBytes() > summary.GetCapacity().GetMemoryBytes() {
		return fmt.Errorf("source allocatable must not exceed capacity.memory_bytes")
	}
	if summary.GetAllocatable() == nil || summary.GetAllocatable().GetMemoryBytes() != EffectiveAllocatable(budget) {
		return fmt.Errorf("allocatable.memory_bytes does not match the memory boundary")
	}
	if summary.GetCollectedAt() == nil {
		return fmt.Errorf("summary collected_at is required")
	}
	if err := summary.GetCollectedAt().CheckValid(); err != nil {
		return fmt.Errorf("invalid summary collected_at: %w", err)
	}
	if budget.GetSampledAt().AsTime().After(summary.GetCollectedAt().AsTime()) {
		return fmt.Errorf("sampled_at cannot be newer than summary.collected_at")
	}
	return nil
}
