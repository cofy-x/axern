package allocation

import (
	"context"
	"fmt"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

// CapabilityReview includes the bounded diagnosis when a running workload must stop.
type CapabilityReview struct {
	Requirements    []*capabilityv1.CapabilityRequirement
	Conditions      *capabilityv1.CapabilityConditionSet
	EnforcementLoss error
}

func (c *Controller) ReviewCapabilities(ctx context.Context, id string, evaluate func(context.Context, string) (CapabilityReview, error), wake func(string)) (CapabilityReview, error) {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return CapabilityReview{}, err
	}
	defer unlock()
	review, err := evaluate(ctx, id)
	if err != nil {
		return CapabilityReview{}, err
	}
	if review.EnforcementLoss != nil {
		if err := c.beginCapabilityTerminationLocked(id, review.EnforcementLoss); err != nil {
			return CapabilityReview{}, fmt.Errorf("persist capability fail-stop ownership: %w", err)
		}
		wake(id)
	}
	return review, nil
}

// A healthy evaluation cannot acknowledge independently committed termination work.
func (c *Controller) AcknowledgeCapabilityEvaluation(ctx context.Context, id string, sequence int64) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if c.CapabilityReconcileState(id).GetTerminating() {
		return nil
	}
	return c.AckCapabilityReconcile(id, sequence, false, nil)
}

func (c *Controller) StopForCapabilityLoss(ctx context.Context, id string) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.failStopWorkloadLocked(ctx, id); err != nil {
		return err
	}
	return c.AckCapabilityReconcile(id, 0, false, nil)
}
