package reservation

import (
	"encoding/json"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMarshalMemoryAdmissionBudgetDisabledDevUsesCanonicalProtoJSON(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	payload, err := marshalMemoryAdmissionBudget(&nodev1.NodeMemoryBudget{
		PhysicalCapacityBytes:     8 << 30,
		SourceAllocatableBytes:    8 << 30,
		EffectiveAllocatableBytes: 8 << 30,
		CapacityIdentity:          "disabled-dev:boot=test:source=node-resources",
		SampledAt:                 timestamppb.New(now),
		Mode:                      nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_DISABLED_DEV,
	})
	if err != nil {
		t.Fatalf("marshalMemoryAdmissionBudget() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal budget JSON: %v", err)
	}
	if decoded["mode"] != "NODE_MEMORY_BUDGET_MODE_DISABLED_DEV" {
		t.Fatalf("mode = %#v", decoded["mode"])
	}
	for _, omittedZero := range []string{"system_reserve_bytes", "internal_current_bytes", "delegated_root_limit_bytes"} {
		if _, ok := decoded[omittedZero]; ok {
			t.Fatalf("canonical protojson unexpectedly emitted zero field %q", omittedZero)
		}
	}
}

func TestMarshalMemoryAdmissionBudgetRejectsAbsentBudget(t *testing.T) {
	if _, err := marshalMemoryAdmissionBudget(nil); err == nil {
		t.Fatal("marshalMemoryAdmissionBudget(nil) succeeded")
	}
}

func TestValidateMemoryAdmissionEvidenceAppliesPublishedBudgetToZeroRequest(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	budget := &nodev1.NodeMemoryBudget{
		PhysicalCapacityBytes: 8 << 30, SourceAllocatableBytes: 8 << 30, SystemReserveBytes: 1 << 30,
		EffectiveAllocatableBytes: 7 << 30, CapacityIdentity: "boot:mount:root:sandbox", SampledAt: timestamppb.New(now),
		Mode: nodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_CGROUP_V2,
	}
	summary := &nodev1.NodeSummary{
		Capacity: &commonv1.ResourceQuantity{MemoryBytes: 8 << 30}, Allocatable: &commonv1.ResourceQuantity{MemoryBytes: 7 << 30},
		MemoryBudget: budget, CollectedAt: timestamppb.New(now),
	}
	evidence := MemoryAdmissionEvidence{
		AllocationID: "alloc-a", Attempt: 1, NodeID: "node-a", Resources: &commonv1.ResourceSpec{}, Summary: summary, AdmittedAt: now,
	}
	if err := validateMemoryAdmissionEvidence(evidence); err != nil {
		t.Fatalf("validateMemoryAdmissionEvidence() error = %v", err)
	}
	budget.SystemReserveExhausted = true
	if err := validateMemoryAdmissionEvidence(evidence); err == nil {
		t.Fatal("validateMemoryAdmissionEvidence() accepted an exhausted system reserve for a zero request")
	}
}

func TestValidateMemoryAdmissionEvidenceRequiresBudgetForPositiveRequest(t *testing.T) {
	err := validateMemoryAdmissionEvidence(MemoryAdmissionEvidence{
		AllocationID: "alloc-a", Attempt: 1, NodeID: "node-a", AdmittedAt: time.Now().UTC(),
		Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{MemoryBytes: 1}},
	})
	if err == nil {
		t.Fatal("validateMemoryAdmissionEvidence() accepted a positive request without a memory budget")
	}
}

func TestValidateMemoryAdmissionEvidenceRequiresBudgetForZeroRequest(t *testing.T) {
	err := validateMemoryAdmissionEvidence(MemoryAdmissionEvidence{
		AllocationID: "alloc-a", Attempt: 1, NodeID: "node-a", AdmittedAt: time.Now().UTC(),
		Resources: &commonv1.ResourceSpec{},
	})
	if err == nil {
		t.Fatal("validateMemoryAdmissionEvidence() accepted a zero request without a memory budget")
	}
}
