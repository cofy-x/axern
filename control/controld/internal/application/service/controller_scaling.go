package appservice

import (
	"context"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
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
