package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var inconclusiveVerificationDelays = []time.Duration{0, 2 * time.Second, 5 * time.Second}

func (h *sandboxService) handleCapabilityTransitions(_ context.Context, transitions []*nodecapability.Transition) {
	if h == nil || len(transitions) == 0 {
		return
	}
	manifests := h.allocationController().CapabilityDependencyManifests()
	for allocationID, dependencies := range manifests {
		for _, transition := range transitions {
			if transition.Current.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE &&
				transition.Previous.GetEvidence().GetEvidenceID() == transition.Current.GetEvidence().GetEvidenceID() {
				continue
			}
			dependency := matchingDependency(dependencies, transition.Key)
			if dependency == nil || dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY {
				continue
			}
			h.startCapabilityReconcile(allocationID, dependency, transition.Current)
		}
	}
}

func (h *sandboxService) startCapabilityReconcile(allocationID string, dependency *capabilityv1.CapabilityDependency, observation *capabilityv1.CapabilityObservation) {
	keyID, err := capabilitycontract.KeyID(dependency.GetKey())
	if err != nil {
		return
	}
	reconcileID := allocationID + "\x00" + keyID
	h.capabilityReconcileMu.Lock()
	if h.capabilityReconciling == nil {
		h.capabilityReconciling = make(map[string]struct{})
	}
	if _, running := h.capabilityReconciling[reconcileID]; running {
		h.capabilityReconcileMu.Unlock()
		return
	}
	h.capabilityReconciling[reconcileID] = struct{}{}
	h.capabilityReconcileMu.Unlock()
	ctx := h.capabilityReconcileCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer func() {
			h.capabilityReconcileMu.Lock()
			delete(h.capabilityReconciling, reconcileID)
			h.capabilityReconcileMu.Unlock()
		}()
		h.reconcileAllocationCapability(ctx, allocationID, dependency, observation)
	}()
}

func (h *sandboxService) reconcileAllocationCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency, observation *capabilityv1.CapabilityObservation) {
	if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE {
		verification := h.verifyAllocationCapability(ctx, allocationID, dependency)
		if verification.State == contract.CapabilityVerificationVerified {
			h.reportCapabilityCondition(allocationID, dependency, observation, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY, "allocation-specific network state remains valid")
		} else {
			h.reportCapabilityCondition(allocationID, dependency, observation, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED, "node capability is degraded: "+verificationMessage(verification))
		}
		return
	}
	verification, err := h.verifyFailStopCapability(ctx, allocationID, dependency)
	if err != nil {
		return
	}
	if verification.State == contract.CapabilityVerificationVerified {
		h.reportCapabilityCondition(allocationID, dependency, observation, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY, "allocation-specific enforcement remains valid")
		return
	}
	h.startCapabilityFailStop(allocationID, dependency, observation, verification.Err)
}

func (h *sandboxService) startCapabilityFailStop(allocationID string, dependency *capabilityv1.CapabilityDependency, observation *capabilityv1.CapabilityObservation, verifyErr error) {
	keyID, err := capabilitycontract.KeyID(dependency.GetKey())
	if err != nil {
		return
	}
	cleanupID := allocationID + "\x00" + keyID + "\x00fail-stop"
	h.capabilityReconcileMu.Lock()
	if h.capabilityReconciling == nil {
		h.capabilityReconciling = make(map[string]struct{})
	}
	if _, running := h.capabilityReconciling[cleanupID]; running {
		h.capabilityReconcileMu.Unlock()
		return
	}
	h.capabilityReconciling[cleanupID] = struct{}{}
	h.capabilityReconcileMu.Unlock()
	ctx := h.capabilityReconcileCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer func() {
			h.capabilityReconcileMu.Lock()
			delete(h.capabilityReconciling, cleanupID)
			h.capabilityReconcileMu.Unlock()
		}()
		h.failStopAllocation(ctx, allocationID, dependency, observation, verifyErr)
	}()
}

func (h *sandboxService) verifyFailStopCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency) (contract.CapabilityVerification, error) {
	return verifyCapabilityWithDelays(ctx, inconclusiveVerificationDelays, func() contract.CapabilityVerification {
		return h.verifyAllocationCapability(ctx, allocationID, dependency)
	})
}

func verifyCapabilityWithDelays(ctx context.Context, delays []time.Duration, verify func() contract.CapabilityVerification) (contract.CapabilityVerification, error) {
	var verification contract.CapabilityVerification
	for index, delay := range delays {
		if delay > 0 {
			previous := time.Duration(0)
			if index > 0 {
				previous = delays[index-1]
			}
			timer := time.NewTimer(delay - previous)
			select {
			case <-ctx.Done():
				timer.Stop()
				return contract.InconclusiveCapability(ctx.Err()), ctx.Err()
			case <-timer.C:
			}
		}
		verification = verify()
		if verification.State != contract.CapabilityVerificationInconclusive {
			return verification, nil
		}
	}
	return verification, nil
}

func (h *sandboxService) verifyAllocationCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency) contract.CapabilityVerification {
	switch dependency.GetKey().GetPlatform() {
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING:
		if len(h.sandboxNetworking().DnatRules(allocationID)) == 0 {
			return contract.LostCapability(fmt.Errorf("allocation has no durable port-forwarding rules"))
		}
		return contract.VerifiedCapability()
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET:
		network, err := h.NetworkForSandbox(allocationID)
		if err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("verify sandbox network resource: %w", err))
		}
		if _, err := os.Stat(network.NetNSPath); err != nil {
			wrapped := fmt.Errorf("verify sandbox network namespace: %w", err)
			if os.IsNotExist(err) {
				return contract.LostCapability(wrapped)
			}
			return contract.InconclusiveCapability(wrapped)
		}
		return contract.VerifiedCapability()
	}
	ct, err := h.containerManager.Get(allocationID)
	if err != nil || ct == nil || ct.Metadata == nil {
		if err == nil {
			err = fmt.Errorf("allocation metadata is unavailable")
		}
		return contract.InconclusiveCapability(fmt.Errorf("load active allocation: %w", err))
	}
	handler, ok := h.runtimeHandlers.Get(ct.Metadata.GetRuntimeHandler())
	if !ok {
		return contract.InconclusiveCapability(fmt.Errorf("runtime handler %q is unavailable", ct.Metadata.GetRuntimeHandler()))
	}
	verifier, ok := handler.(contract.AllocationCapabilityVerifier)
	if !ok {
		return contract.LostCapability(fmt.Errorf("runtime %q has no allocation capability verifier", handler.Name()))
	}
	cgroupPath := ""
	memoryLimit := int64(0)
	platform := dependency.GetKey().GetPlatform()
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT {
		cgroupPath, err = h.containerManager.RuntimeCgroupPath(allocationID)
		if err != nil {
			return contract.InconclusiveCapability(err)
		}
	}
	if ct.Status != nil {
		memoryLimit = ct.Status.Get().LinuxResources.GetMemoryLimitInBytes()
	}
	return verifier.VerifyAllocationCapability(ctx, dependency, contract.HandlerOptions{
		ContainerID: allocationID, CgroupPath: cgroupPath, RuntimeCgroupPath: cgroupPath, MemoryLimitBytes: memoryLimit,
	})
}

func verificationMessage(verification contract.CapabilityVerification) string {
	if verification.Err == nil {
		return "capability enforcement could not be proven"
	}
	return verification.Err.Error()
}

func (h *sandboxService) failStopAllocation(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency, observation *capabilityv1.CapabilityObservation, verifyErr error) {
	message := "CAPABILITY_ENFORCEMENT_LOST: " + strings.TrimSpace(verificationMessage(contract.CapabilityVerification{Err: verifyErr}))
	runtimeName := "unknown"
	if ct, err := h.containerManager.Get(allocationID); err == nil && ct != nil && ct.Metadata != nil {
		runtimeName = ct.Metadata.GetRuntimeHandler()
	}
	metrics.RecordCapabilityAllocationVerification(runtimeName, "fail_stop")
	h.reportCapabilityCondition(allocationID, dependency, observation, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED, message)
	for {
		_, err := h.allocationController().Delete(ctx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 10})
		if err == nil {
			return
		}
		logrus.WithError(err).WithField("allocation_id", allocationID).Error("retry fail-stop allocation cleanup")
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (h *sandboxService) reportCapabilityCondition(allocationID string, dependency *capabilityv1.CapabilityDependency, observation *capabilityv1.CapabilityObservation, state capabilityv1.CapabilityConditionState, message string) {
	ct, err := h.containerManager.Get(allocationID)
	if err != nil || ct == nil || ct.Metadata == nil {
		return
	}
	attempt := controlplane.AllocationAttemptFromLabels(ct.Metadata.GetLabels())
	if attempt <= 0 {
		return
	}
	status := commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING
	if state == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED {
		status = commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	}
	var evidence *capabilityv1.CapabilityEvidence
	if observation != nil {
		evidence = observation.GetEvidence()
	}
	if evidence == nil {
		evidence = dependency.GetSelectedEvidence()
	}
	reasonCode := capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST
	if state == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY {
		reasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
	}
	h.controlPlaneReports.ReportCapabilityConditions(allocationID, attempt, status, strings.TrimSpace(message), []*capabilityv1.CapabilityCondition{{
		Key: proto.Clone(dependency.GetKey()).(*capabilityv1.CapabilityKey), State: state,
		ReasonCode: reasonCode,
		Message:    strings.TrimSpace(message), ObservedAt: timestamppb.Now(), Evidence: cloneCapabilityEvidence(evidence),
	}})
}

func matchingDependency(dependencies []*capabilityv1.CapabilityDependency, key *capabilityv1.CapabilityKey) *capabilityv1.CapabilityDependency {
	want, err := capabilitycontract.KeyID(key)
	if err != nil {
		return nil
	}
	for _, dependency := range dependencies {
		id, err := capabilitycontract.KeyID(dependency.GetKey())
		if err == nil && id == want {
			return dependency
		}
	}
	return nil
}

// ReconcileAllocationCapabilities is the controld safety-net entrypoint. The
// node-local transition reconciler remains primary; this method makes the same
// allocation-specific proof available after either side restarts or misses a
// transition notification.
func (h *sandboxService) ReconcileAllocationCapabilities(ctx context.Context, allocationID string) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	dependencies := h.allocationController().CapabilityDependencyManifests()[strings.TrimSpace(allocationID)]
	if len(dependencies) == 0 {
		return nil, nil, nil
	}
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(dependencies))
	now := time.Now().UTC()
	snapshot := h.capabilityManager.Snapshot()
	for _, dependency := range dependencies {
		observation, available := capabilitycontract.AvailableObservation(snapshot, dependency.GetKey(), now)
		state := capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY
		code := capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
		message := "allocation capability remains available"
		if !available {
			switch dependency.GetLossPolicy() {
			case capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY:
				state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY
				message = "admission-only capability no longer affects the running allocation"
			case capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE:
				verification := h.verifyAllocationCapability(ctx, allocationID, dependency)
				if verification.State != contract.CapabilityVerificationVerified {
					state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
					code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST
					message = "node capability is degraded: " + verificationMessage(verification)
				} else {
					message = "allocation-specific network state remains valid"
				}
			case capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP:
				verification, verifyErr := h.verifyFailStopCapability(ctx, allocationID, dependency)
				if verifyErr != nil {
					return nil, nil, fmt.Errorf("verify allocation capability %q: %w", allocationID, verifyErr)
				}
				if verification.State != contract.CapabilityVerificationVerified {
					state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED
					code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST
					message = "CAPABILITY_ENFORCEMENT_LOST: " + verificationMessage(verification)
					h.startCapabilityFailStop(allocationID, dependency, observation, verification.Err)
				} else {
					message = "allocation-specific enforcement remains valid"
				}
			}
		}
		evidence := dependency.GetSelectedEvidence()
		if observation != nil && observation.GetEvidence() != nil {
			evidence = observation.GetEvidence()
		}
		conditions = append(conditions, &capabilityv1.CapabilityCondition{
			Key: proto.Clone(dependency.GetKey()).(*capabilityv1.CapabilityKey), State: state, ReasonCode: code,
			Message: message, ObservedAt: timestamppb.New(now), Evidence: cloneCapabilityEvidence(evidence),
		})
	}
	return dependencies, conditions, nil
}

func cloneCapabilityEvidence(evidence *capabilityv1.CapabilityEvidence) *capabilityv1.CapabilityEvidence {
	if evidence == nil {
		return nil
	}
	return proto.Clone(evidence).(*capabilityv1.CapabilityEvidence)
}
