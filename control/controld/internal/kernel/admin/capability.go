package adminkernel

import (
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type CapabilityTransition struct {
	TransitionID, NodeID, SnapshotID string
	SnapshotSequence                 int64
	Key                              *capabilityv1.CapabilityKey
	OldState, NewState               capabilityv1.CapabilityState
	OldEvidenceID, NewEvidenceID     string
	ReasonCode                       capabilityv1.CapabilityReasonCode
	Reason                           string
	ObservedAt, ReportedAt           time.Time
}

type CapabilityReconcileItem struct {
	AllocationID, NodeID string
	Dependencies         []*capabilityv1.CapabilityDependency
	Attempts             int32
	NextRunAt            time.Time
	LeaseExpiresAt       *time.Time
	LastError            string
	UpdatedAt            time.Time
}

type AllocationCapabilityDiagnostics struct {
	AllocationID, NodeID string
	Dependencies         []*capabilityv1.CapabilityDependency
	AdmittedDependencies []*capabilityv1.CapabilityDependency
	Conditions           []*capabilityv1.CapabilityCondition
	Reconcile            *CapabilityReconcileItem
}
