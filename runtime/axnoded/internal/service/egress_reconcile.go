package service

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

func (h *sandboxService) reconcileEgressPolicies(ctx context.Context) error {
	if h.egressClient == nil {
		return nil
	}
	allocationIDs := h.allocationController().AllocationIDs()
	if _, err := h.egressClient.Reconcile(ctx, allocationIDs); err != nil {
		if len(allocationIDs) == 0 {
			logrus.WithError(err).Warn("egressd unavailable; unrestricted allocation recovery continues")
			return nil
		}
		return fmt.Errorf("reconcile active egress policies: %w", err)
	}
	return nil
}
