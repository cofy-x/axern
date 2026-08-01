package appservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *controller) ReconcilePending(ctx context.Context, now time.Time) error {
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        ctrlobs.SpanServiceReconcilePending,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "service_reconcile_pending")},
		Counter:     ctrlobs.MetricReconcileTotal,
		Duration:    ctrlobs.MetricReconcileDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	services, err := c.store.List(ctx, &servicev1.ServiceListFilter{Statuses: []servicev1.ServiceStatus{
		servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
	}})
	if err != nil {
		opErr = errors.Join(opErr, err)
		return opErr
	}
	autoscaled, err := c.autoscaledServices(ctx)
	if err != nil {
		opErr = errors.Join(opErr, err)
		return opErr
	}
	pending := make([]*servicev1.Service, 0, len(services)+len(autoscaled))
	seen := make(map[string]struct{}, len(services)+len(autoscaled))
	for _, service := range services {
		if service == nil || service.GetDeletionStatus().GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
			continue
		}
		seen[service.GetID()] = struct{}{}
		pending = append(pending, service)
	}
	for _, service := range autoscaled {
		if _, ok := seen[service.GetID()]; ok {
			continue
		}
		pending = append(pending, service)
	}
	opErr = errors.Join(opErr, c.reconcileServices(ctx, pending, now))
	return opErr
}

func (c *controller) ReconcileAutoscaled(ctx context.Context, now time.Time) error {
	services, err := c.autoscaledServices(ctx)
	if err != nil {
		return err
	}
	return c.reconcileServices(ctx, services, now)
}

func (c *controller) autoscaledServices(ctx context.Context) ([]*servicev1.Service, error) {
	if c.autoscaling == nil {
		return nil, nil
	}
	services, err := c.autoscaling.ListAutoscaled(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*servicev1.Service, 0, len(services))
	for _, service := range services {
		if service != nil && service.GetAutoscalingPolicy() != nil {
			out = append(out, service)
		}
	}
	return out, nil
}

func (c *controller) ReconcileServices(ctx context.Context, serviceIDs []string, now time.Time) error {
	unique := make(map[string]struct{}, len(serviceIDs))
	for _, serviceID := range serviceIDs {
		serviceID = strings.TrimSpace(serviceID)
		if serviceID != "" {
			unique[serviceID] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(unique))
	for serviceID := range unique {
		ordered = append(ordered, serviceID)
	}
	sort.Strings(ordered)
	services := make([]*servicev1.Service, 0, len(ordered))
	for _, serviceID := range ordered {
		services = append(services, &servicev1.Service{ID: serviceID})
	}
	return c.reconcileServices(ctx, services, now)
}

func (c *controller) reconcileServices(ctx context.Context, services []*servicev1.Service, now time.Time) error {
	if len(services) == 0 {
		return nil
	}
	concurrency := c.reconcileConcurrency
	if concurrency <= 0 {
		concurrency = defaultServiceReconcileConcurrency
	}
	if concurrency > len(services) {
		concurrency = len(services)
	}

	batchStarted := time.Now()
	jobs := make(chan *servicev1.Service, len(services))
	results := make(chan error, len(services))
	for _, service := range services {
		jobs <- service
	}
	close(jobs)

	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for service := range jobs {
				c.recordReconcileStage(ctx, serviceReconcileStageQueueWait, batchStarted, nil)
				syncStarted := time.Now()
				_, err := c.Sync(ctx, service, now)
				c.recordReconcileStage(ctx, serviceReconcileStageSync, syncStarted, err)
				c.recordReconcileStage(ctx, serviceReconcileStageTotal, batchStarted, err)
				results <- err
			}
		}()
	}
	workers.Wait()
	close(results)

	var retErr error
	for err := range results {
		retErr = errors.Join(retErr, err)
	}
	return retErr
}

func (c *controller) Sync(ctx context.Context, service *servicev1.Service, now time.Time) (*servicev1.Service, error) {
	if service == nil {
		return nil, nil
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: ctrlobs.SpanServiceSync,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrServiceID, service.GetID()),
		},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "service_sync")},
		Counter:     ctrlobs.MetricReconcileTotal,
		Duration:    ctrlobs.MetricReconcileDuration,
	})
	var opErr error
	defer func() {
		if opErr != nil {
			logrus.WithError(opErr).WithFields(logrus.Fields{
				"service_id": service.GetID(),
				"trace_id":   sdkobs.TraceFields(ctx)["trace_id"],
			}).Warn("service sync failed")
		}
		op.End(opErr)
	}()
	lockStarted := time.Now()
	unlock := c.syncLocks.lock(service.GetID())
	c.recordReconcileStage(ctx, serviceReconcileStageLockWait, lockStarted, nil)
	defer unlock()

	current := service
	if c.store != nil {
		refreshed, found, err := c.store.Get(ctx, service.GetID())
		if err != nil {
			opErr = err
			return current, err
		}
		if !found {
			return nil, nil
		}
		current = refreshed
	}
	if current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		next, err := c.syncDeleted(ctx, current, now)
		opErr = err
		return next, err
	}
	effectiveDesired, current, err := c.evaluateAutoscaling(ctx, current, now)
	if err != nil {
		opErr = err
		return current, err
	}
	env, err := c.environments.GetEnvironment(ctx, current.GetEnvironmentID())
	if err != nil {
		opErr = err
		return current, err
	}
	desired := effectiveDesired
	allocations, err := c.allocations.CurrentServiceAllocations(ctx, current.GetID())
	if err != nil {
		opErr = err
		return current, err
	}
	rolloutHandled, next, err := c.reconcileRollout(ctx, current, env, allocations, desired, now)
	if err != nil {
		opErr = err
		return current, err
	}
	current = next
	if !rolloutHandled {
		current, allocations, err = c.scaleDown(ctx, current, allocations, desired, now)
		if err != nil {
			opErr = err
			return current, err
		}
		current, allocations, err = c.scaleUp(ctx, current, env, allocations, desired, now)
		if err != nil {
			opErr = err
			return current, err
		}
	}
	next, err = c.statuses.SyncObservedStatus(ctx, current.GetID(), now)
	if err != nil {
		opErr = err
		return current, err
	}
	if next == nil {
		return nil, nil
	}
	next, err = c.applyRolloutProgressMessage(ctx, next, now)
	if err != nil {
		opErr = err
		return next, err
	}
	if next.GetRolloutStatus() != nil {
		op.SetAttributes(attribute.String(sdkobs.AttrDiagnosticCode, next.GetRolloutStatus().GetDiagnosticCode().String()))
	}
	return next, nil
}

func (c *controller) syncDeleted(ctx context.Context, current *servicev1.Service, now time.Time) (*servicev1.Service, error) {
	if current.GetDeletionStatus().GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		return current, nil
	}
	allocations, err := c.allocations.ServiceAllocationHistory(ctx, current.GetID())
	if err != nil {
		return current, err
	}
	current, _, err = c.scaleDown(ctx, current, allocationsRequiringRelease(allocations), 0, now)
	if err != nil {
		return current, err
	}
	next, err := c.statuses.SyncObservedStatus(ctx, current.GetID(), now)
	if err != nil {
		return current, err
	}
	if next == nil {
		return nil, nil
	}
	if hasUnreleasedAllocations(allocations) {
		return next, nil
	}
	deletion := next.GetDeletionStatus()
	if deletion == nil {
		return current, fmt.Errorf("service %q is missing deletion status", next.GetID())
	}
	if deletion.GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		return next, nil
	}
	if c.storage == nil {
		return current, fmt.Errorf("service storage coordinator is required for volume disposition")
	}
	if deletion.GetVolumeDisposition() != servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE {
		// Retained volumes stay re-attachable: release the owner so a
		// future workload can claim the same claim and backend.
		if _, err := c.storage.ReleaseWorkloadVolumeClaims(ctx, next.GetNamespace(), next.GetID()); err != nil {
			return current, err
		}
		return c.completeServiceDeletion(ctx, next, nil, now)
	}
	result, err := c.storage.DeleteWorkloadVolumeClaims(ctx, next.GetNamespace(), next.GetID())
	if err != nil {
		return current, err
	}
	claimIDs := append([]string(nil), deletion.GetClaimIds()...)
	for _, claimID := range result.GetClaimIds() {
		claimIDs = appendUniqueString(claimIDs, claimID)
	}
	if result.GetComplete() {
		return c.completeServiceDeletion(ctx, next, claimIDs, now)
	}
	return c.statuses.UpdateDeletionStatus(ctx, next.GetID(), &servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES,
		VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
		ClaimIds:          claimIDs, Message: "reclaiming service volumes",
	}, now)
}

func hasUnreleasedAllocations(allocations []*servicekernel.AllocationRecord) bool {
	for _, allocation := range allocations {
		if allocation != nil && allocation.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED {
			return true
		}
	}
	return false
}

func allocationTargetForNode(allocations []*servicekernel.AllocationRecord, nodeID string) string {
	for i := len(allocations) - 1; i >= 0; i-- {
		if allocations[i] != nil && allocations[i].NodeID == nodeID && allocations[i].NodeTarget != "" {
			return allocations[i].NodeTarget
		}
	}
	return ""
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if value != "" {
		return append(values, value)
	}
	return values
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (c *controller) completeServiceDeletion(ctx context.Context, service *servicev1.Service, claimIDs []string, now time.Time) (*servicev1.Service, error) {
	disposition := servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN
	if service.GetDeletionStatus() != nil {
		disposition = service.GetDeletionStatus().GetVolumeDisposition()
	}
	// Persist the terminal audit event before making the tombstone complete.
	// A failed status write may produce a duplicate event on retry, but it can
	// never leave a terminal deletion without its completion audit record.
	if err := c.recordEvent(ctx, servicekernel.NewServiceEvent(service.GetID(), "",
		servicev1.ServiceEventType_SERVICE_EVENT_TYPE_DELETION_COMPLETED,
		servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED,
		service.GetDiagnosticCode(), "service deletion and volume disposition complete", now)); err != nil {
		return service, err
	}
	return c.statuses.UpdateDeletionStatus(ctx, service.GetID(), &servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
		VolumeDisposition: disposition, ClaimIds: claimIDs, Message: "service deletion complete", CompletedAt: timestamppb.New(now),
	}, now)
}

func allocationsRequiringRelease(allocations []*servicekernel.AllocationRecord) []*servicekernel.AllocationRecord {
	pending := make([]*servicekernel.AllocationRecord, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation != nil &&
			allocation.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING &&
			allocation.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED {
			pending = append(pending, allocation)
		}
	}
	return pending
}

func (c *controller) syncAfterWrite(ctx context.Context, service *servicev1.Service, err error, now time.Time) (*servicev1.Service, error) {
	if err != nil || service == nil {
		return service, err
	}
	return c.Sync(ctx, service, now)
}

func (c *controller) applyRolloutProgressMessage(ctx context.Context, current *servicev1.Service, now time.Time) (*servicev1.Service, error) {
	if current == nil {
		return current, nil
	}
	allocations, err := c.allocations.CurrentServiceAllocations(ctx, current.GetID())
	if err != nil {
		return current, err
	}
	rollout := servicekernel.NewRolloutState(current, allocations)
	current.RolloutStatus = servicekernel.BuildRolloutStatus(current, allocations)
	if current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING || current.GetRolloutStatus() == nil {
		return current, nil
	}
	next, err := c.statuses.UpdateStatus(ctx, current.GetID(), current.GetStatus(), rollout.ProgressMessage(), now)
	if err != nil {
		return current, err
	}
	next.RolloutStatus = current.GetRolloutStatus()
	return next, nil
}
