package servicekernel

import (
	"context"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

type EnvironmentReader interface {
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
}

type CandidateSelector interface {
	SelectCandidates(ctx context.Context, env *environmentv1.Environment, config *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error)
}

type StorageCoordinator interface {
	ResolveRequirements(ctx context.Context, namespace, serviceID string, config *commonv1.ExecutionConfig) ([]*privatestoragev1.VolumeRequirement, error)
	ReserveBindings(ctx context.Context, req StorageReserveRequest) ([]*privatestoragev1.ResolvedNodeVolume, error)
	ReportBindingPublish(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.PublishedNodeVolume) error
	ReportBindingPublishFailed(ctx context.Context, allocationID, nodeID string, volumes []*privatestoragev1.ResolvedNodeVolume, message string) error
	ReportBindingRelease(ctx context.Context, allocationID, nodeID string, observations []*privatestoragev1.VolumeReleaseObservation) error
	DeleteWorkloadVolumeClaims(ctx context.Context, namespace, serviceID string) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error)
	ReleaseWorkloadVolumeClaims(ctx context.Context, namespace, serviceID string) (*privatestoragev1.ReleaseWorkloadVolumeClaimsResponse, error)
	ClaimVolumeReclaims(ctx context.Context, leaseOwner string, excludedNodeIDs []string) (*privatestoragev1.VolumeReclaim, error)
	ReportVolumeReclaim(ctx context.Context, reclaim *privatestoragev1.VolumeReclaim, succeeded bool, message string) error
}

type VolumeReclaimDispatcher interface {
	RunVolumeReclaimDispatcher(ctx context.Context, leaseOwner string, workers, workersPerNode int)
}

type StorageReserveRequest struct {
	Namespace    string
	ServiceID    string
	AllocationID string
	NodeID       string
	Config       *commonv1.ExecutionConfig
}

type AllocationLifecycle interface {
	CreateResolvedAllocation(ctx context.Context, req CreateResolvedAllocationRequest) (*CreateResolvedAllocationResult, error)
	DeleteResolvedAllocation(ctx context.Context, target, allocationID string, attempt int64, nodeID string) ([]*privatestoragev1.VolumeReleaseObservation, error)
	AllocationDeleted(ctx context.Context, target, allocationID string, attempt int64, nodeID string) (bool, error)
	DeleteVolume(ctx context.Context, target string, reclaim *privatestoragev1.VolumeReclaim) error
}

type CreateResolvedAllocationResult struct {
	PublishedVolumes               []*privatestoragev1.PublishedNodeVolume
	WorkspacePreparation           *commonv1.WorkspacePreparationFacts
	CapabilityVerification         []*capabilityv1.CapabilityCondition
	AdmittedCapabilityDependencies []*capabilityv1.CapabilityDependency
}

// CreateResolvedAllocationRequest is the resolved node-lifecycle payload shape
// that service reconciliation sends to nodebridge. It stays object-shaped
// because it combines several coordinated inputs that should not be passed as a
// long positional list.
type CreateResolvedAllocationRequest struct {
	Target                 string
	Namespace              string
	ServiceID              string
	AllocationID           string
	Attempt                int64
	Config                 *commonv1.ExecutionConfig
	Environment            *environmentv1.Environment
	NodeID                 string
	ReadinessProbe         *servicev1.ServiceProbe
	LivenessProbe          *servicev1.ServiceProbe
	NodeVolumes            []*privatestoragev1.ResolvedNodeVolume
	CapabilityDependencies []*capabilityv1.CapabilityDependency
}
