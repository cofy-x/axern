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
	DeleteAllocation(context.Context, string, *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error)
}

type Queue interface {
	Claim(context.Context, string, int, time.Time, time.Duration) ([]allocationkernel.CapabilityReconcileItem, error)
	Complete(context.Context, string, string) error
	Retry(context.Context, allocationkernel.CapabilityReconcileItem, string, time.Time, error) error
	RecordAdmission(context.Context, allocationkernel.CapabilityReconcileItem, string, *allocationkernel.CapabilityAdmission, time.Time) error
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
		return r.queue.Complete(ctx, item.AllocationID, r.owner)
	}
	if err != nil {
		return err
	}
	conditions := response.GetCapabilityVerification()
	admission := &allocationkernel.CapabilityAdmission{
		Dependencies: response.GetAdmittedCapabilityDependencies(),
		Conditions:   conditions,
	}
	if err := r.queue.RecordAdmission(ctx, item, r.owner, admission, now); err != nil {
		return err
	}
	if hasFailedCondition(conditions) {
		sdkobs.Int64Counter(ctrlobs.MetricCapabilityFailStopTotal.Name, ctrlobs.MetricCapabilityFailStopTotal.Description).Add(ctx, 1,
			attribute.String(sdkobs.AttrReason, "enforcement_lost"),
		)
		if _, err := r.client.DeleteAllocation(ctx, item.NodeTarget, &privatenodev1.DeleteAllocationRequest{
			AllocationID: item.AllocationID, Attempt: item.Attempt, NodeID: item.NodeID, TimeoutSeconds: 10,
		}); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("force-delete allocation after capability loss: %w", err)
		}
		// Keep the durable queue item until a subsequent status check proves the
		// runtime is gone. A successful delete RPC only confirms that cleanup was
		// accepted; it does not make the allocation safe to forget.
		return fmt.Errorf("capability fail-stop delete issued; awaiting deletion confirmation")
	}
	return r.queue.Complete(ctx, item.AllocationID, r.owner)
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
