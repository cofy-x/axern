package adminkernel

import (
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type AllocationCapabilityDiagnostics struct {
	AllocationID, NodeID string
	Requirements         []*capabilityv1.CapabilityRequirement
	ConditionSet         *capabilityv1.CapabilityConditionSet
}
