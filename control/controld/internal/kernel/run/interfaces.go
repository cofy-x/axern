package runkernel

import (
	"context"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

type AllocationRecord struct {
	AllocationID           string
	NodeID                 string
	NodeTarget             string
	CapabilityRequirements []*capabilityv1.CapabilityRequirement
}

type EnvironmentStore interface {
	CreateEnvironment(ctx context.Context, params CreateEnvironmentParams, now time.Time) (*environmentv1.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
	ListEnvironments(ctx context.Context, filter *environmentv1.ListFilter) ([]*environmentv1.Environment, string, error)
	DeleteEnvironment(ctx context.Context, id string, now time.Time) (*environmentv1.Environment, error)
}

type RunStore interface {
	AdmitRun(ctx context.Context, params AdmitRunParams, now time.Time) (*runv1.Run, error)
	GetRun(ctx context.Context, id string) (*runv1.Run, error)
	WatchRun(ctx context.Context, id string, afterVersion int64) (*runv1.Run, error)
	ListRuns(ctx context.Context, filter *runv1.RunListFilter) ([]*runv1.Run, string, error)
	CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, error)
}

type CreateEnvironmentParams struct {
	Spec         *environmentv1.EnvironmentSpec
	ResolvedSpec *catalogv1.ResolvedEnvironmentSpec
	Labels       map[string]string
}

type CreateParams struct {
	Namespace     string
	EnvironmentID string
	Config        *commonv1.ExecutionConfig
	Labels        map[string]string
}

// AdmitRunParams groups the user-shaped run admission input. The candidate set
// is part of the same admission decision, so it stays in this object instead of
// trailing as another positional argument.
type AdmitRunParams struct {
	Namespace   string
	Environment *environmentv1.Environment
	Config      *commonv1.ExecutionConfig
	Labels      map[string]string
	Candidates  []*placementkernel.Candidate
}

type LeaseStore interface {
	IssueExecutionLease(ctx context.Context, allocationID string, ttl time.Duration, now time.Time) (*leasekernel.IssuedGrant, error)
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*leasekernel.Record, int64, error)
}

type AllocationReporter interface {
	BatchReportAllocationLifecycle(ctx context.Context, nodeID string, observations []*nodev1.AllocationLifecycleObservation, now time.Time) error
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error
}

type ReconcileStore interface {
	LoadStartAllocation(ctx context.Context, allocationID string) (*StartAllocation, error)
	CompleteAllocationStart(ctx context.Context, allocationID, claimOwner string, conditions *capabilityv1.CapabilityConditionSet, now time.Time) error
	CompleteAllocationRelease(ctx context.Context, allocationID, claimOwner string, now time.Time) error
	MarkAllocationCreateFailed(ctx context.Context, allocationID, claimOwner string, message string, now time.Time) (*runv1.Run, error)
	ClaimDueReconcileItems(ctx context.Context, owner string, limit int, now time.Time, leaseTTL time.Duration) ([]allocationkernel.ReconcileItem, error)
	RenewReconcileClaim(ctx context.Context, allocationID, owner string, now time.Time, leaseTTL time.Duration) (bool, error)
	ScheduleClaimedReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error)
	WaitReconcileWork(ctx context.Context) error
}

type StartAllocation struct {
	Run         *runv1.Run
	Environment *environmentv1.Environment
	Allocation  *AllocationRecord
}
