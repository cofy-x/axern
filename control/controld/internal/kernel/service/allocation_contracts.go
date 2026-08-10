package servicekernel

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type AllocationReporter interface {
	BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*nodev1.AllocationStatusObservation, now time.Time) (AllocationStatusBatchResult, error)
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error
}

type AllocationStore interface {
	CurrentServiceAllocations(ctx context.Context, serviceID string) ([]*AllocationRecord, error)
	ServiceAllocationHistory(ctx context.Context, serviceID string) ([]*AllocationRecord, error)
	AdmitAllocation(ctx context.Context, serviceID string, config *commonv1.ExecutionConfig, candidates []*placementkernel.Candidate, now time.Time) (*servicev1.Service, *AllocationRecord, error)
	BeginAllocationRelease(ctx context.Context, serviceID, allocationID string, now time.Time) (*servicev1.Service, *AllocationRecord, error)
	MarkAllocationCreateFailed(ctx context.Context, serviceID, allocationID, message string, now time.Time) (*servicev1.Service, error)
	RecordWorkspacePreparation(ctx context.Context, serviceID, allocationID string, attempt int64, facts *commonv1.WorkspacePreparationFacts, now time.Time) error
	RecordCapabilityAdmission(ctx context.Context, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error
	CompleteAllocationRelease(ctx context.Context, allocationID string, now time.Time) error
	CompleteClaimedAllocationRelease(ctx context.Context, allocationID, owner string, now time.Time) (bool, error)
}

type AllocationReconcileStore interface {
	DueReconcileItems(ctx context.Context, limit int, now time.Time) ([]allocationkernel.ReconcileItem, error)
	ClaimDueReconcileItems(ctx context.Context, owner string, limit int, now time.Time, leaseTTL time.Duration) ([]allocationkernel.ReconcileItem, error)
	RenewReconcileClaim(ctx context.Context, allocationID, owner string, now time.Time, leaseTTL time.Duration) (bool, error)
	ScheduleReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, now time.Time) error
	ScheduleClaimedReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error)
	CompleteAllocationCreate(ctx context.Context, allocationID string, now time.Time) error
	CompleteClaimedAllocationCreate(ctx context.Context, allocationID, owner string, now time.Time) (bool, error)
}

type AllocationRecord struct {
	AllocationID           string
	ServiceID              string
	DesiredSpecDigest      string
	EnvironmentID          string
	NodeID                 string
	NodeTarget             string
	Attempt                int64
	Status                 commonv1.AllocationStatus
	Ready                  bool
	ReadinessMessage       string
	ReadinessProbe         *servicev1.ServiceProbe
	LivenessProbe          *servicev1.ServiceProbe
	Config                 *commonv1.ExecutionConfig
	CapabilityDependencies []*capabilityv1.CapabilityDependency
}

type AllocationStatusReport struct {
	ReplicaBecameReady        bool
	ServiceBecameReady        bool
	ReplicaReadyDuration      time.Duration
	ServiceReadyDuration      time.Duration
	ReplicaReadyDurationKnown bool
	ServiceReadyDurationKnown bool
}

type AllocationStatusBatchResult struct {
	Reports             []*AllocationStatusReport
	ReconcileServiceIDs []string
}
