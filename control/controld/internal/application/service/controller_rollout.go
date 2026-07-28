package appservice

import (
	"context"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func (c *controller) reconcileRollout(ctx context.Context, current *servicev1.Service, env *environmentv1.Environment, allocations []*servicekernel.AllocationRecord, desired int, now time.Time) (bool, *servicev1.Service, error) {
	if current == nil || desired <= 0 {
		return false, current, nil
	}
	rollout := servicekernel.NewRolloutState(current, allocations)
	if !rollout.InProgress() {
		return false, current, nil
	}
	if rollout.CanAdmitReplacement() {
		return c.admitRolloutReplacement(ctx, current, env, now)
	}
	if removable := rollout.RemovableOutdatedAllocation(); removable != nil {
		return c.drainOutdatedAllocation(ctx, current, removable, now)
	}
	return true, current, nil
}

func (c *controller) admitRolloutReplacement(ctx context.Context, current *servicev1.Service, env *environmentv1.Environment, now time.Time) (bool, *servicev1.Service, error) {
	stageStarted := time.Now()
	candidates, err := c.selector.SelectCandidates(ctx, env, current.GetConfig())
	c.recordReplicaStage(ctx, serviceReplicaPathRolloutReplacement, serviceReplicaStageSelectCandidates, stageStarted, err)
	if err != nil {
		if !serviceAdmissionBlocked(err) {
			return true, current, err
		}
		current, err = c.reportFailure(ctx, current, "", err.Error(), servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED, true, now)
		if err != nil {
			return true, current, err
		}
		return true, current, nil
	}
	stageStarted = time.Now()
	candidates, err = c.filterStorageCandidates(ctx, current, candidates)
	c.recordReplicaStage(ctx, serviceReplicaPathRolloutReplacement, serviceReplicaStageFilterStorageCandidates, stageStarted, err)
	if err != nil {
		if !serviceAdmissionBlocked(err) {
			return true, current, err
		}
		current, err = c.reportFailure(ctx, current, "", err.Error(), servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED, true, now)
		if err != nil {
			return true, current, err
		}
		return true, current, nil
	}
	stageStarted = time.Now()
	next, alloc, err := c.allocations.AdmitAllocation(ctx, current.GetID(), current.GetConfig(), candidates, now)
	c.recordReplicaStage(ctx, serviceReplicaPathRolloutReplacement, serviceReplicaStageAdmitAllocation, stageStarted, err)
	if err != nil {
		if !serviceAdmissionBlocked(err) {
			return true, next, err
		}
		current, err = c.reportFailure(ctx, current, "", err.Error(), servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED, true, now)
		if err != nil {
			return true, current, err
		}
		return true, current, nil
	}
	current = next
	if emitErr := c.recordEvent(ctx, servicekernel.NewServiceEvent(
		current.GetID(),
		alloc.AllocationID,
		servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_ADMITTED,
		servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT,
		commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		"replacement replica admitted",
		now,
	)); emitErr != nil {
		return true, current, emitErr
	}
	return true, current, nil
}

func (c *controller) drainOutdatedAllocation(ctx context.Context, current *servicev1.Service, removable *servicekernel.AllocationRecord, now time.Time) (bool, *servicev1.Service, error) {
	next, _, err := c.allocations.BeginAllocationRelease(ctx, current.GetID(), removable.AllocationID, now)
	if err != nil {
		return true, next, err
	}
	return true, next, nil
}
