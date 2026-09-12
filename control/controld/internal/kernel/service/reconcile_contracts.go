package servicekernel

import (
	"context"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type EnvironmentReader interface {
	GetEnvironment(ctx context.Context, id string) (*environmentv1.Environment, error)
}

type CandidateSelector interface {
	SelectCandidates(ctx context.Context, env *environmentv1.Environment, config *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error)
}

type AllocationLifecycle interface {
	CreateResolvedAllocation(ctx context.Context, req CreateResolvedAllocationRequest) (*CreateResolvedAllocationResult, error)
	DeleteResolvedAllocation(ctx context.Context, target, allocationID string, attempt int64, nodeID string) error
	AllocationDeleted(ctx context.Context, target, allocationID string, attempt int64, nodeID string) (bool, error)
}

type CreateResolvedAllocationResult struct {
	WorkspacePreparation           *commonv1.WorkspacePreparationFacts
	CapabilityVerification         *capabilityv1.CapabilityConditionSet
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
	CapabilityDependencies []*capabilityv1.CapabilityDependency
}
