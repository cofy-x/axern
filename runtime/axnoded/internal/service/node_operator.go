package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) ValidateOperatorExecution(allocationID string) error {
	if err := h.allocationController().ValidateOperatorExecution(allocationID, time.Now().UTC()); err != nil {
		return err
	}
	target, err := h.sandboxTargetResolver().Running(strings.TrimSpace(allocationID))
	if err != nil {
		return err
	}
	if target.ID != strings.TrimSpace(allocationID) || target.Container == nil || target.Container.ID != target.ID || target.Handler == nil {
		return fmt.Errorf("allocation %q runtime identity does not match AllocationState", allocationID)
	}
	return nil
}

func (h *sandboxService) ValidateOperatorInspection(allocationID string) error {
	allocationID = strings.TrimSpace(allocationID)
	if err := h.allocationController().ValidateOperatorInspection(allocationID); err != nil {
		return err
	}
	target, err := h.sandboxTargetResolver().Container(allocationID)
	if err != nil {
		return err
	}
	if target.ID != allocationID || target.Container == nil || target.Container.ID != target.ID || target.Handler == nil {
		return fmt.Errorf("allocation %q runtime identity does not match AllocationState", allocationID)
	}
	return nil
}

func (h *sandboxService) ForceTerminateAllocation(ctx context.Context, allocationID, reason string) (err error) {
	allocationID = strings.TrimSpace(allocationID)
	reason = strings.TrimSpace(reason)
	if err := h.ValidateOperatorInspection(allocationID); err != nil {
		return err
	}
	if reason == "" {
		return fmt.Errorf("break-glass reason is required")
	}
	fields := logrus.Fields{"allocation_id": allocationID, "operator_action": "force_terminate", "reason": reason}
	logrus.WithFields(fields).Warn("node operator break-glass action requested")
	if err := h.recordBreakGlassTermination(allocationID, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_OPERATOR_FORCE_TERMINATED, reason); err != nil {
		return err
	}
	defer func() {
		entry := logrus.WithFields(fields)
		if err != nil {
			entry.WithError(err).Error("node operator break-glass action failed")
		} else {
			entry.Warn("node operator break-glass action accepted")
		}
	}()
	_, err = h.Kill(ctx, &runtimev1.KillRequest{ID: allocationID, Signal: "KILL"})
	return err
}

func (h *sandboxService) ForceCleanupAllocation(ctx context.Context, allocationID, reason string, timeoutSeconds int64) (err error) {
	allocationID = strings.TrimSpace(allocationID)
	reason = strings.TrimSpace(reason)
	if err := h.ValidateOperatorInspection(allocationID); err != nil {
		return err
	}
	if reason == "" {
		return fmt.Errorf("break-glass reason is required")
	}
	fields := logrus.Fields{"allocation_id": allocationID, "operator_action": "force_cleanup", "reason": reason}
	logrus.WithFields(fields).Warn("node operator break-glass action requested")
	if err := h.recordBreakGlassTermination(allocationID, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_OPERATOR_FORCE_CLEANUP, reason); err != nil {
		return err
	}
	defer func() {
		entry := logrus.WithFields(fields)
		if err != nil {
			entry.WithError(err).Error("node operator break-glass action failed")
		} else {
			entry.Warn("node operator break-glass action completed")
		}
	}()
	_, err = h.Delete(ctx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: timeoutSeconds})
	return err
}

func (h *sandboxService) recordBreakGlassTermination(allocationID string, code commonv1.WorkloadDiagnosticCode, reason string) error {
	existingCode, _ := h.allocationController().TerminationIntent(allocationID)
	if existingCode != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		// Preserve the first durable cause. A retry or later cleanup action must
		// not rewrite already established terminal evidence.
		return nil
	}
	return h.allocationController().MarkTerminationIntent(allocationID, code, reason)
}

func (h *sandboxService) ResolveAllocationNetwork(allocationID string) (*SandboxNetwork, error) {
	if err := h.allocationController().ValidateAllocationNetworkResolution(allocationID, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := h.ValidateOperatorExecution(allocationID); err != nil {
		return nil, err
	}
	return h.NetworkForSandbox(strings.TrimSpace(allocationID))
}
