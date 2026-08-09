package appservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	allocationReconcileLeaseTTL       = 30 * time.Second
	allocationReconcileRenewInterval  = 10 * time.Second
	allocationReconcileRefillInterval = 10 * time.Millisecond
)

type allocationReconcileResult struct {
	nodeID string
	err    error
}

type allocationReconcileClaim struct {
	item        allocationkernel.ReconcileItem
	claimedAt   time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	stopRenewal chan struct{}
	renewalErr  chan error
	renewalDone chan struct{}
}

func (c *controller) ReconcileAllocationBatch(ctx context.Context, now time.Time) (int, error) {
	if c.reconcile == nil {
		return 0, nil
	}
	globalConcurrency := c.allocationGlobalConcurrency
	if globalConcurrency <= 0 {
		globalConcurrency = defaultAllocationCreateGlobalConcurrency
	}
	perNodeConcurrency := c.allocationPerNodeConcurrency
	if perNodeConcurrency <= 0 {
		perNodeConcurrency = defaultAllocationCreatePerNodeConcurrency
	}
	results := make(chan allocationReconcileResult, globalConcurrency)
	pending := make([]*allocationReconcileClaim, 0, globalConcurrency)
	activeByNode := make(map[string]int)
	active := 0
	processed := 0
	var reconcileErr error
	claiming := true
	defer c.recordAllocationDispatcherCurrent(ctx, 0, 0)

	for {
		available := globalConcurrency - active - len(pending)
		if available > 0 && claiming {
			owner := "controld-" + uuid.NewString()
			claimStarted := time.Now()
			items, err := c.reconcile.ClaimDueReconcileItems(ctx, owner, available, time.Now(), allocationReconcileLeaseTTL)
			claimedAt := time.Now()
			c.recordServiceAllocationQueue(ctx, "batch", serviceAllocationQueueStageClaimStore, claimedAt.Sub(claimStarted), err)
			if err != nil {
				reconcileErr = errors.Join(reconcileErr, err)
				claiming = false
			}
			for _, item := range items {
				if !item.NextRunAt.IsZero() {
					c.recordServiceAllocationQueue(ctx, allocationReconcilePath(item), serviceAllocationQueueStageDueLag, nonnegativeDuration(claimedAt.Sub(item.NextRunAt)), nil)
				}
				if !item.EligibleAt.IsZero() {
					c.recordServiceAllocationQueue(ctx, allocationReconcilePath(item), serviceAllocationQueueStageClaimWait, nonnegativeDuration(claimedAt.Sub(item.EligibleAt)), nil)
				}
				pending = append(pending, c.startAllocationReconcileClaim(ctx, item, claimedAt))
			}
		}

		remaining := make([]*allocationReconcileClaim, 0, len(pending))
		for _, claim := range pending {
			if active >= globalConcurrency || activeByNode[claim.item.NodeID] >= perNodeConcurrency {
				remaining = append(remaining, claim)
				continue
			}
			active++
			activeByNode[claim.item.NodeID]++
			dispatchedAt := time.Now()
			path := allocationReconcilePath(claim.item)
			c.recordServiceAllocationQueue(ctx, path, serviceAllocationQueueStageDispatcherWait, dispatchedAt.Sub(claim.claimedAt), nil)
			if !claim.item.EligibleAt.IsZero() {
				c.recordServiceAllocationQueue(ctx, path, serviceAllocationQueueStageTotal, nonnegativeDuration(dispatchedAt.Sub(claim.item.EligibleAt)), nil)
			}
			go func() {
				err := c.reconcileClaimedAllocation(claim.ctx, claim.item, now)
				results <- allocationReconcileResult{nodeID: claim.item.NodeID, err: errors.Join(err, claim.finish())}
			}()
		}
		pending = remaining
		c.recordAllocationDispatcherCurrent(ctx, active, len(pending))
		if active == 0 {
			if len(pending) == 0 {
				return processed, reconcileErr
			}
			return processed, errors.Join(reconcileErr, errors.New("allocation dispatcher cannot schedule claimed work"))
		}

		timer := time.NewTimer(allocationReconcileRefillInterval)
		select {
		case result := <-results:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			active--
			activeByNode[result.nodeID]--
			if activeByNode[result.nodeID] == 0 {
				delete(activeByNode, result.nodeID)
			}
			c.recordAllocationDispatcherCurrent(ctx, active, len(pending))
			processed++
			reconcileErr = errors.Join(reconcileErr, result.err)
		case <-timer.C:
		}
	}
}

func nonnegativeDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}

func (c *controller) startAllocationReconcileClaim(ctx context.Context, item allocationkernel.ReconcileItem, claimedAt time.Time) *allocationReconcileClaim {
	timeout := allocationkernel.LifecycleOperationTimeout
	if item.Reason == allocationkernel.ReconcileReasonCreate {
		timeout = allocationkernel.CreateExecutionTimeout
	}
	workerCtx, cancel := context.WithTimeout(ctx, timeout)
	claim := &allocationReconcileClaim{
		item:        item,
		claimedAt:   claimedAt,
		ctx:         workerCtx,
		cancel:      cancel,
		stopRenewal: make(chan struct{}),
		renewalErr:  make(chan error, 1),
		renewalDone: make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(allocationReconcileRenewInterval)
		defer ticker.Stop()
		defer close(claim.renewalDone)
		for {
			select {
			case <-ticker.C:
				renewed, err := c.reconcile.RenewReconcileClaim(workerCtx, item.AllocationID, item.ClaimOwner, time.Now(), allocationReconcileLeaseTTL)
				if err != nil {
					claim.renewalErr <- err
					cancel()
					return
				}
				if !renewed {
					claim.renewalErr <- errors.New("allocation reconcile claim lost")
					cancel()
					return
				}
			case <-claim.stopRenewal:
				return
			case <-workerCtx.Done():
				return
			}
		}
	}()
	return claim
}

func (c *allocationReconcileClaim) finish() error {
	close(c.stopRenewal)
	<-c.renewalDone
	c.cancel()
	select {
	case err := <-c.renewalErr:
		return err
	default:
		return nil
	}
}

func (c *controller) reconcileClaimedAllocation(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	var err error
	switch item.Reason {
	case allocationkernel.ReconcileReasonCreate:
		err = c.reconcileAllocationCreate(ctx, item, now)
	case allocationkernel.ReconcileReasonDelete:
		err = c.reconcileAllocationDelete(ctx, item, now)
	default:
		err = fmt.Errorf("unsupported allocation reconcile reason %q", item.Reason)
	}
	if c.notifyReconcile != nil && item.OwnerID != "" {
		c.notifyReconcile(item.OwnerID)
	}
	return err
}

func allocationReconcilePath(item allocationkernel.ReconcileItem) string {
	if item.Reason == allocationkernel.ReconcileReasonDelete {
		return serviceReplicaPathReconcileDelete
	}
	return serviceReplicaPathReconcileCreate
}

func (c *controller) reconcileAllocationCreate(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	if item.OwnerID == "" || item.EnvironmentID == "" {
		return c.completeClaimedAllocationCreate(ctx, item, now)
	}
	service, ok, err := c.store.Get(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	if !ok || service == nil {
		return c.completeClaimedAllocationCreate(ctx, item, now)
	}
	if !containsAllocationID(service.GetAllocationIds(), item.AllocationID) {
		return c.completeClaimedAllocationCreate(ctx, item, now)
	}
	env, err := c.environments.GetEnvironment(ctx, item.EnvironmentID)
	if err != nil {
		return err
	}
	stageStarted := time.Now()
	nodeVolumes, err := c.reserveStorage(ctx, service, &servicekernel.AllocationRecord{
		AllocationID: item.AllocationID,
		ServiceID:    item.OwnerID,
		NodeID:       item.NodeID,
		NodeTarget:   item.NodeTarget,
		Attempt:      item.Attempt,
	})
	c.recordReplicaStage(ctx, serviceReplicaPathReconcileCreate, serviceReplicaStageReserveStorage, stageStarted, err)
	if err != nil {
		if req, ok := allocationkernel.ScheduleCreateRetryRequest(item.AllocationID, item.ReconcileAttempts, err.Error(), now); ok {
			return c.scheduleClaimedAllocationReconcile(ctx, item, req, now)
		}
		if _, markErr := c.allocations.MarkAllocationCreateFailed(ctx, service.GetID(), item.AllocationID, err.Error(), now); markErr != nil {
			return markErr
		}
		return c.completeClaimedAllocationCreate(ctx, item, now)
	}
	stageStarted = time.Now()
	createResult, err := c.lifecycle.CreateResolvedAllocation(ctx, servicekernel.CreateResolvedAllocationRequest{
		Target:                 item.NodeTarget,
		Namespace:              service.GetNamespace(),
		ServiceID:              service.GetID(),
		AllocationID:           item.AllocationID,
		Attempt:                item.Attempt,
		Config:                 service.GetConfig(),
		Environment:            env,
		NodeID:                 item.NodeID,
		ReadinessProbe:         service.GetReadinessProbe(),
		LivenessProbe:          service.GetLivenessProbe(),
		NodeVolumes:            nodeVolumes,
		CapabilityDependencies: item.CapabilityDependencies,
	})
	c.recordReplicaStage(ctx, serviceReplicaPathReconcileCreate, serviceReplicaStageNodeCreateAllocation, stageStarted, err)
	if err != nil {
		message := storagePublishFailureMessage(nodeVolumes, err)
		if reportErr := c.reportStoragePublishFailed(ctx, item.AllocationID, item.NodeID, nodeVolumes, message); reportErr != nil {
			return reportErr
		}
		if grpcstatus.Code(err) == codes.ResourceExhausted {
			if _, markErr := c.allocations.MarkAllocationCreateFailed(ctx, service.GetID(), item.AllocationID, message, now); markErr != nil {
				return markErr
			}
			return c.completeClaimedAllocationCreate(ctx, item, now)
		}
		if req, ok := allocationkernel.ScheduleCreateRetryRequest(item.AllocationID, item.ReconcileAttempts, message, now); ok {
			return c.scheduleClaimedAllocationReconcile(ctx, item, req, now)
		}
		if _, markErr := c.allocations.MarkAllocationCreateFailed(ctx, service.GetID(), item.AllocationID, message, now); markErr != nil {
			return markErr
		}
		return c.completeClaimedAllocationCreate(ctx, item, now)
	}
	stageStarted = time.Now()
	if createResult == nil {
		createResult = &servicekernel.CreateResolvedAllocationResult{}
	}
	if err := c.allocations.RecordCapabilityVerification(ctx, item.AllocationID, &allocationkernel.CapabilityAdmission{
		Dependencies: createResult.AdmittedCapabilityDependencies,
		Conditions:   createResult.CapabilityVerification,
	}, now); err != nil {
		return err
	}
	if err := c.reportStoragePublished(ctx, item.AllocationID, item.NodeID, createResult.PublishedVolumes); err != nil {
		c.recordReplicaStage(ctx, serviceReplicaPathReconcileCreate, serviceReplicaStageReportStoragePublished, stageStarted, err)
		return err
	}
	c.recordReplicaStage(ctx, serviceReplicaPathReconcileCreate, serviceReplicaStageReportStoragePublished, stageStarted, nil)
	if err := c.allocations.RecordWorkspacePreparation(ctx, service.GetID(), item.AllocationID, item.Attempt, createResult.WorkspacePreparation, now); err != nil {
		return err
	}
	stageStarted = time.Now()
	err = c.completeClaimedAllocationCreate(ctx, item, now)
	c.recordReplicaStage(ctx, serviceReplicaPathReconcileCreate, serviceReplicaStageCompleteAllocationCreate, stageStarted, err)
	return err
}

func (c *controller) reconcileAllocationDelete(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	alloc := &servicekernel.AllocationRecord{
		AllocationID:  item.AllocationID,
		ServiceID:     item.OwnerID,
		EnvironmentID: item.EnvironmentID,
		NodeID:        item.NodeID,
		NodeTarget:    item.NodeTarget,
		Attempt:       item.Attempt,
		Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING,
	}
	deleted, err := c.deleteAndConfirmAllocation(ctx, alloc)
	if err != nil || !deleted {
		message := ""
		if err != nil {
			message = err.Error()
		}
		return c.scheduleClaimedAllocationReconcile(ctx, item, allocationkernel.ScheduleDeleteRetryRequest(item.AllocationID, message, now), now)
	}
	completed, err := c.allocations.CompleteClaimedAllocationRelease(ctx, item.AllocationID, item.ClaimOwner, now)
	if err != nil {
		return err
	}
	if !completed {
		return fmt.Errorf("allocation reconcile claim lost for %s", item.AllocationID)
	}
	return nil
}

func (c *controller) scheduleClaimedAllocationReconcile(ctx context.Context, item allocationkernel.ReconcileItem, req allocationkernel.ScheduleReconcileRequest, now time.Time) error {
	updated, err := c.reconcile.ScheduleClaimedReconcile(ctx, req, item.ClaimOwner, now)
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("allocation reconcile claim lost for %s", item.AllocationID)
	}
	return nil
}

func (c *controller) completeClaimedAllocationCreate(ctx context.Context, item allocationkernel.ReconcileItem, now time.Time) error {
	deleted, err := c.reconcile.CompleteClaimedAllocationCreate(ctx, item.AllocationID, item.ClaimOwner, now)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("allocation reconcile claim lost for %s", item.AllocationID)
	}
	return nil
}

func containsAllocationID(allocationIDs []string, allocationID string) bool {
	for _, current := range allocationIDs {
		if current == allocationID {
			return true
		}
	}
	return false
}
