package adminkernel

import (
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type CapabilityTransition struct {
	TransitionID, NodeID, SnapshotID string
	SnapshotSequence                 int64
	Key                              *capabilityv1.CapabilityKey
	OldState, NewState               capabilityv1.CapabilityState
	OldEvidence, NewEvidence         *capabilityv1.CapabilityEvidence
	OldReasonCode, NewReasonCode     capabilityv1.CapabilityReasonCode
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
	AllocationID, NodeID      string
	Attempt                   int64
	CreateAdmissionRecorded   bool
	CreateDependencySetDigest string
	CreateAdmittedAt          *time.Time
	Dependencies              []*capabilityv1.CapabilityDependency
	AdmittedDependencies      []*capabilityv1.CapabilityDependency
	ConditionSet              *capabilityv1.CapabilityConditionSet
	Reconcile                 *CapabilityReconcileItem
	MemoryAdmission           *AllocationMemoryAdmission
	LatestMemoryObservation   *nodev1.AllocationMemoryObservation
}

type AllocationMemoryAdmission struct {
	SandboxMemoryRequestBytes int64
	SandboxMemoryLimitBytes   int64
	NodeMemoryBudget          *nodev1.NodeMemoryBudget
	SummaryCollectedAt        time.Time
	NodeLocalCommitmentBytes  int64
	AdmittedAt                time.Time
}
