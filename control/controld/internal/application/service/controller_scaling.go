package appservice

import (
	"context"
	"fmt"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (c *controller) scaleDown(ctx context.Context, current *servicev1.Service, allocations []*servicekernel.AllocationRecord, desired int, now time.Time) (*servicev1.Service, []*servicekernel.AllocationRecord, error) {
	for len(allocations) > desired {
		alloc := allocations[len(allocations)-1]
		next, _, err := c.allocations.BeginAllocationRelease(ctx, current.GetID(), alloc.AllocationID, now)
		if err != nil {
			return next, allocations, err
		}
		current = next
		allocations = allocations[:len(allocations)-1]
	}
	return current, allocations, nil
}

func (c *controller) scaleUp(ctx context.Context, current *servicev1.Service, env *environmentv1.Environment, allocations []*servicekernel.AllocationRecord, desired int, now time.Time) (*servicev1.Service, []*servicekernel.AllocationRecord, error) {
	resolvedConfig, err := executionkernel.NormalizeConfigForRootfs(current.GetConfig(), env.GetResolvedTemplate().GetRootfsReadonly())
	if err != nil {
		return current, allocations, err
	}
	for len(allocations) < desired {
		stageStarted := time.Now()
		candidates, err := c.selector.SelectCandidates(ctx, env, resolvedConfig)
		c.recordReplicaStage(ctx, serviceReplicaPathScaleUp, serviceReplicaStageSelectCandidates, stageStarted, err)
		if err != nil {
			return c.handleScaleUpAdmissionError(ctx, current, allocations, err, now)
		}
		stageStarted = time.Now()
		candidates, err = c.filterStorageCandidates(ctx, current, candidates)
		c.recordReplicaStage(ctx, serviceReplicaPathScaleUp, serviceReplicaStageFilterStorageCandidates, stageStarted, err)
		if err != nil {
			return c.handleScaleUpAdmissionError(ctx, current, allocations, err, now)
		}
		stageStarted = time.Now()
		next, alloc, err := c.allocations.AdmitAllocation(ctx, current.GetID(), resolvedConfig, candidates, now)
		c.recordReplicaStage(ctx, serviceReplicaPathScaleUp, serviceReplicaStageAdmitAllocation, stageStarted, err)
		if err != nil {
			return c.handleScaleUpAdmissionError(ctx, current, allocations, err, now)
		}
		current = next
		allocations = append(allocations, alloc)
	}
	return current, allocations, nil
}

func (c *controller) handleScaleUpAdmissionError(ctx context.Context, current *servicev1.Service, allocations []*servicekernel.AllocationRecord, err error, now time.Time) (*servicev1.Service, []*servicekernel.AllocationRecord, error) {
	if !serviceAdmissionBlocked(err) {
		return current, allocations, err
	}
	next, reportErr := c.reportFailure(ctx, current, "", err.Error(), servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED, false, now)
	if reportErr != nil {
		return current, allocations, reportErr
	}
	return next, allocations, nil
}

func (c *controller) filterStorageCandidates(ctx context.Context, current *servicev1.Service, candidates []*placementkernel.Candidate) ([]*placementkernel.Candidate, error) {
	if len(current.GetConfig().GetVolumeMounts()) == 0 {
		return candidates, nil
	}
	if c.storage == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "service volume mounts require a storage coordinator")
	}
	requirements, err := c.storage.ResolveRequirements(ctx, current.GetNamespace(), current.GetID(), current.GetConfig())
	if err != nil {
		return nil, err
	}
	out := make([]*placementkernel.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if requirementsAllowNode(requirements, candidate.NodeID) {
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "service volume topology unsatisfied: no placement candidates satisfy required volume topology")
	}
	return out, nil
}

func requirementsAllowNode(requirements []*privatestoragev1.VolumeRequirement, nodeID string) bool {
	for _, req := range requirements {
		requiredNode := req.GetTopology().GetNodeID()
		if requiredNode != "" && requiredNode != nodeID {
			return false
		}
	}
	return true
}

func (c *controller) reserveStorage(ctx context.Context, current *servicev1.Service, alloc *servicekernel.AllocationRecord) ([]*privatestoragev1.ResolvedNodeVolume, error) {
	if len(current.GetConfig().GetVolumeMounts()) == 0 {
		return nil, nil
	}
	if c.storage == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "service volume mounts require a storage coordinator")
	}
	volumes, err := c.storage.ReserveBindings(ctx, servicekernel.StorageReserveRequest{
		Namespace:    current.GetNamespace(),
		ServiceID:    current.GetID(),
		AllocationID: alloc.AllocationID,
		NodeID:       alloc.NodeID,
		Config:       current.GetConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("storage reserve failed: %w", err)
	}
	return volumes, nil
}
