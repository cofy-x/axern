package memorybudget

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateAcceptsFiniteDelegatedRootBudget(t *testing.T) {
	budget := validBudget()
	budget.DelegatedRootLimitFinite = true
	budget.DelegatedRootLimitBytes = 6 << 30
	budget.EffectiveAllocatableBytes = 5 << 30
	if err := Validate(budget); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateAcceptsDisabledDevelopmentCapacityWithoutClaimingCgroupAccounting(t *testing.T) {
	budget := &nodev1.NodeMemoryBudget{
		PhysicalCapacityBytes:     8 << 30,
		SourceAllocatableBytes:    6 << 30,
		EffectiveAllocatableBytes: 6 << 30,
		CapacityIdentity:          "disabled-dev:boot=test:source=node-resources",
		SampledAt:                 timestamppb.New(time.Now().UTC()),
		Mode:                      nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_DISABLED_DEV,
	}
	if err := Validate(budget); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	budget.InternalCurrentBytes = 1
	if err := Validate(budget); err == nil {
		t.Fatal("Validate() accepted disabled_dev internal cgroup accounting")
	}
}

func TestPublishedRequiresAConcreteBudgetObservation(t *testing.T) {
	if Published(nil) || Published(&nodev1.NodeMemoryBudget{}) {
		t.Fatal("empty budget was published as a memory capacity observation")
	}
	if !Published(validBudget()) {
		t.Fatal("valid cgroup memory budget was not published")
	}
}

func TestValidateRejectsInconsistentOrAmbiguousBudgets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*nodev1.NodeMemoryBudget)
	}{
		{name: "wrong effective allocatable", mutate: func(b *nodev1.NodeMemoryBudget) { b.EffectiveAllocatableBytes++ }},
		{name: "cleanup exceeds commitment", mutate: func(b *nodev1.NodeMemoryBudget) { b.CleanupDebtBytes = b.LocalCommitmentBytes + 1 }},
		{name: "unbounded root with limit", mutate: func(b *nodev1.NodeMemoryBudget) { b.DelegatedRootLimitBytes = 1 }},
		{name: "source exceeds physical", mutate: func(b *nodev1.NodeMemoryBudget) { b.SourceAllocatableBytes = b.PhysicalCapacityBytes + 1 }},
		{name: "missing identity", mutate: func(b *nodev1.NodeMemoryBudget) { b.CapacityIdentity = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := validBudget()
			tt.mutate(budget)
			if err := Validate(budget); err == nil {
				t.Fatal("Validate() accepted invalid budget")
			}
		})
	}
}

func TestValidateAtRejectsStaleAndFutureSamples(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for _, sampledAt := range []time.Time{
		now.Add(-SampleFreshness - time.Nanosecond),
		now.Add(maxFutureSkew + time.Nanosecond),
	} {
		budget := validBudget()
		budget.SampledAt = timestamppb.New(sampledAt)
		if err := ValidateAt(budget, now); err == nil {
			t.Fatalf("ValidateAt() accepted sample time %s", sampledAt)
		}
	}
	budget := validBudget()
	budget.SampledAt = timestamppb.New(now.Add(-SampleFreshness))
	if err := ValidateAt(budget, now); err != nil {
		t.Fatalf("ValidateAt() rejected boundary sample: %v", err)
	}
}

func TestValidateSummaryBindsEffectiveAllocatableAndSampleGeneration(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	budget := validBudget()
	budget.SampledAt = timestamppb.New(now)
	summary := &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(now), MemoryBudget: budget,
		Capacity:    &commonv1.ResourceQuantity{MemoryBytes: budget.GetPhysicalCapacityBytes()},
		Allocatable: &commonv1.ResourceQuantity{MemoryBytes: budget.GetEffectiveAllocatableBytes()},
	}
	if err := ValidateSummary(summary, now); err != nil {
		t.Fatalf("ValidateSummary() error = %v", err)
	}
	summary.Allocatable.MemoryBytes++
	if err := ValidateSummary(summary, now); err == nil {
		t.Fatal("ValidateSummary() accepted mismatched allocatable")
	}
	summary.Allocatable.MemoryBytes--
	summary.Capacity.MemoryBytes++
	if err := ValidateSummary(summary, now); err == nil {
		t.Fatal("ValidateSummary() accepted mismatched physical capacity")
	}
	summary.Capacity.MemoryBytes--
	summary.CollectedAt = timestamppb.New(now.Add(-time.Second))
	if err := ValidateSummary(summary, now); err == nil {
		t.Fatal("ValidateSummary() accepted a sample newer than its summary")
	}
}

func validBudget() *nodev1.NodeMemoryBudget {
	return &nodev1.NodeMemoryBudget{
		PhysicalCapacityBytes: 8 << 30, SourceAllocatableBytes: 8 << 30, SystemReserveBytes: 1 << 30,
		EffectiveAllocatableBytes: 7 << 30, LocalCommitmentBytes: 2 << 30,
		CleanupDebtBytes: 1 << 30, InternalCurrentBytes: 256 << 20,
		CapacityIdentity: "boot:mount:root:sandbox", SampledAt: timestamppb.New(time.Now().UTC()),
		Mode: nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_CGROUP_V2,
	}
}
