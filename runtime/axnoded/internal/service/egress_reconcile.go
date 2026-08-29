package service

import (
	"context"
	"fmt"

	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) reconcileEgressPolicies(ctx context.Context) error {
	if h.egressClient == nil {
		return nil
	}
	manifests := h.allocationController().EgressPolicyProofs()
	active := make([]*runtimeegressv1.ActiveEgressPolicy, 0, len(manifests))
	for allocationID, manifest := range manifests {
		active = append(active, &runtimeegressv1.ActiveEgressPolicy{
			AllocationID: allocationID, Attempt: manifest.Attempt, SandboxIp: manifest.Proof.GetSandboxIp(), PolicyDigest: manifest.Proof.GetPolicyDigest(), ExecutionRevision: manifest.Proof.GetExecutionRevision(),
		})
	}
	if _, err := h.egressClient.Reconcile(ctx, active); err != nil {
		if len(active) == 0 {
			logrus.WithError(err).Warn("egressd unavailable; unrestricted allocation recovery continues")
			return nil
		}
		return fmt.Errorf("reconcile active egress policies: %w", err)
	}
	return nil
}
