package capability

import (
	"context"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LifecycleClient interface {
	GetAllocationStatus(context.Context, string, *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error)
}

type Queue interface {
	Claim(context.Context, string, int, time.Time, time.Duration) ([]allocationkernel.CapabilityReconcileItem, error)
	Complete(context.Context, allocationkernel.CapabilityReconcileItem, string, time.Time) error
	Retry(context.Context, allocationkernel.CapabilityReconcileItem, string, time.Time, error) error
	RecordConditions(context.Context, allocationkernel.CapabilityReconcileItem, string, *allocationkernel.CapabilityReconciliation, time.Time) error
}

type Reconciler struct {
	queue  Queue
	client LifecycleClient
	owner  string
}

func NewReconciler(queue Queue, client LifecycleClient) *Reconciler {
	return newReconciler(queue, client, "controld-capability-"+uuid.NewString())
}

func newReconciler(queue Queue, client LifecycleClient, owner string) *Reconciler {
	return &Reconciler{queue: queue, client: client, owner: owner}
}

func (r *Reconciler) Reconcile(ctx context.Context, now time.Time) error {
	items, err := r.queue.Claim(ctx, r.owner, 32, now, 30*time.Second)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := r.reconcileOne(ctx, item, now); err != nil {
			recordReconcile(ctx, "retry")
			if retryErr := r.queue.Retry(ctx, item, r.owner, now, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		recordReconcile(ctx, "complete")
	}
	return nil
}

func (r *Reconciler) reconcileOne(ctx context.Context, item allocationkernel.CapabilityReconcileItem, now time.Time) error {
	response, err := r.client.GetAllocationStatus(ctx, item.NodeTarget, &privatenodev1.GetAllocationStatusRequest{
		AllocationID: item.AllocationID, Attempt: item.Attempt, NodeID: item.NodeID,
	})
	if status.Code(err) == codes.NotFound {
		return r.queue.Complete(ctx, item, r.owner, now)
	}
	if err != nil {
		return err
	}
	conditionSet := response.GetCapabilityVerification()
	reconciliation := &allocationkernel.CapabilityReconciliation{
		Attempt:      item.Attempt,
		Dependencies: response.GetAdmittedCapabilityDependencies(),
		ConditionSet: conditionSet,
	}
	if err := r.queue.RecordConditions(ctx, item, r.owner, reconciliation, now); err != nil {
		return err
	}
	if hasFailedCondition(conditionSet.GetConditions()) {
		sdkobs.Int64Counter(ctrlobs.MetricCapabilityFailStopTotal.Name, ctrlobs.MetricCapabilityFailStopTotal.Description).Add(ctx, 1,
			attribute.String(sdkobs.AttrReason, "enforcement_lost"),
		)
		// GetAllocationStatus persists node-local termination ownership before it
		// returns a failed condition. Controld deliberately does not become a
		// second Delete initiator; the durable queue remains the restart and
		// missed-report safety net until the node proves the allocation is gone.
		return fmt.Errorf("capability fail-stop is owned by the node; awaiting deletion confirmation")
	}
	return r.queue.Complete(ctx, item, r.owner, now)
}

func recordReconcile(ctx context.Context, result string) {
	sdkobs.Int64Counter(ctrlobs.MetricCapabilityReconcileTotal.Name, ctrlobs.MetricCapabilityReconcileTotal.Description).Add(ctx, 1,
		attribute.String(sdkobs.AttrResult, result),
	)
}

func hasFailedCondition(conditions []*capabilityv1.CapabilityCondition) bool {
	for _, condition := range conditions {
		if condition.GetState() == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED {
			return true
		}
	}
	return false
}
