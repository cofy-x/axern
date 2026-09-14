package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var inconclusiveVerificationDelays = []time.Duration{0, 2 * time.Second, 5 * time.Second}

const (
	capabilityReconcileRetryDelay = 2 * time.Second
	// Runtime audits read controls, identities, and PID membership only. One
	// deterministic shard is scheduled per tick so every allocation is covered
	// over ten minutes without a node-wide 60-second verification storm. The
	// scheduler advances a monotonic shard cursor, so a delayed tick extends the
	// sweep instead of silently skipping work.
	capabilityAuditTick        = 5 * time.Second
	capabilityAuditShardCount  = 120
	capabilityReconcileWorkers = 4
)

func (h *sandboxService) handleCapabilityTransitions(_ context.Context, transitions []*nodecapability.Transition) {
	if h == nil || len(transitions) == 0 {
		return
	}
	manifests := h.allocationController().CapabilityRequirementManifests()
	for allocationID, dependencies := range manifests {
		keys := make([]*capabilityv1.CapabilityKey, 0, len(transitions))
		sequence := int64(0)
		for _, transition := range transitions {
			dependency := matchingDependency(dependencies, transition.Key)
			if dependency == nil || dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY {
				continue
			}
			keys = append(keys, capabilitycontract.CloneKey(transition.Key))
			if transition.Sequence > sequence {
				sequence = transition.Sequence
			}
		}
		if len(keys) == 0 {
			continue
		}
		if err := h.allocationController().MergeCapabilityReconcile(allocationID, sequence); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("persist capability reconcile work")
			continue
		}
		h.startCapabilityReconcileWorker(allocationID)
	}
}

func matchingDependency(requirements []*capabilityv1.CapabilityRequirement, key *capabilityv1.CapabilityKey) *capabilityv1.CapabilityRequirement {
	want, err := capabilitycontract.KeyID(key)
	if err != nil {
		return nil
	}
	for _, requirement := range requirements {
		id, err := capabilitycontract.KeyID(requirement.GetKey())
		if err == nil && id == want {
			return requirement
		}
	}
	return nil
}

// startCapabilityReconcileWorker establishes allocation-level single ownership.
// New observation sequences are persisted before this call and are picked up
// by the running worker's next loop rather than being dropped.
func (h *sandboxService) startCapabilityReconcileWorker(allocationID string) {
	if !h.acquireCapabilityReconcileWorker(allocationID) {
		return
	}
	ctx := h.capabilityReconcileCtx
	if ctx == nil {
		ctx = context.Background()
	}
	h.capabilityReconcileWG.Add(1)
	go func() {
		defer h.capabilityReconcileWG.Done()
		defer func() {
			h.finishCapabilityReconcileWorker(allocationID)
			if ctx.Err() == nil {
				// Close the enqueue/exit race and use the released node-wide budget
				// to resume durable work in stable allocation order.
				h.startPendingCapabilityReconcileWorkers()
			}
		}()
		h.runCapabilityReconcileWorker(ctx, allocationID)
	}()
}

func (h *sandboxService) acquireCapabilityReconcileWorker(allocationID string) bool {
	h.capabilityReconcileMu.Lock()
	defer h.capabilityReconcileMu.Unlock()
	if h.capabilityReconciling == nil {
		h.capabilityReconciling = make(map[string]bool)
	}
	if _, running := h.capabilityReconciling[allocationID]; running {
		return false
	}
	if h.capabilityReconcileActive >= capabilityReconcileWorkers {
		// Work is durable. A completing worker or the next audit tick resumes it.
		return false
	}
	h.capabilityReconciling[allocationID] = true
	h.capabilityReconcileActive++
	return true
}

// releaseCapabilityReconcileBudget keeps the per-allocation termination owner
// while returning its verification permit. Cleanup may retry indefinitely and
// must not prevent another allocation from proving and enforcing a hard-limit
// loss.
func (h *sandboxService) releaseCapabilityReconcileBudget(allocationID string) {
	h.capabilityReconcileMu.Lock()
	if holdsBudget, running := h.capabilityReconciling[allocationID]; running && holdsBudget {
		h.capabilityReconciling[allocationID] = false
		h.capabilityReconcileActive--
	}
	h.capabilityReconcileMu.Unlock()
}

func (h *sandboxService) finishCapabilityReconcileWorker(allocationID string) {
	h.capabilityReconcileMu.Lock()
	if holdsBudget, running := h.capabilityReconciling[allocationID]; running {
		if holdsBudget {
			h.capabilityReconcileActive--
		}
		delete(h.capabilityReconciling, allocationID)
	}
	h.capabilityReconcileMu.Unlock()
}

func (h *sandboxService) startPendingCapabilityReconcileWorkers() {
	manifests := h.allocationController().CapabilityRequirementManifests()
	allocationIDs := make([]string, 0, len(manifests))
	for allocationID := range manifests {
		allocationIDs = append(allocationIDs, allocationID)
	}
	sort.Strings(allocationIDs)
	for _, allocationID := range allocationIDs {
		state := h.allocationController().CapabilityReconcileState(allocationID)
		if state == nil || (!state.GetTerminating() && state.GetPendingObservationSequence() == 0) {
			continue
		}
		h.startCapabilityReconcileWorker(allocationID)
	}
}

func (h *sandboxService) startPeriodicCapabilityAudit() {
	ctx := h.capabilityReconcileCtx
	if ctx == nil {
		return
	}
	h.capabilityReconcileWG.Add(1)
	go func() {
		defer h.capabilityReconcileWG.Done()
		h.startPendingCapabilityReconcileWorkers()
		auditShard := capabilityAuditShardAt(time.Now().UTC())
		ticker := time.NewTicker(capabilityAuditTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			h.startPendingCapabilityReconcileWorkers()
			currentAuditShard := auditShard
			auditShard = nextCapabilityAuditShard(auditShard)
			snapshot := h.capabilityManager.Snapshot()
			sequence := snapshot.GetSequence()
			if sequence <= 0 {
				continue
			}
			for allocationID, dependencies := range h.allocationController().CapabilityRequirementManifests() {
				if capabilityAuditShard(allocationID) != currentAuditShard {
					continue
				}
				keys := periodicCapabilityAuditKeys(dependencies)
				if len(keys) == 0 {
					continue
				}
				if err := h.allocationController().MergeCapabilityReconcile(allocationID, sequence); err != nil {
					logrus.WithError(err).WithField("allocation_id", allocationID).Warn("persist periodic capability audit")
					continue
				}
				h.startCapabilityReconcileWorker(allocationID)
			}
		}
	}()
}

func capabilityAuditShard(allocationID string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(allocationID))
	return hash.Sum32() % capabilityAuditShardCount
}

func capabilityAuditShardAt(now time.Time) uint32 {
	return uint32(now.UnixNano()/int64(capabilityAuditTick)) % capabilityAuditShardCount
}

func nextCapabilityAuditShard(current uint32) uint32 {
	return (current + 1) % capabilityAuditShardCount
}

// The sharded audit is a cheap safety net for missed transitions and silent
// control drift. Verifiers may read kernel/runtime controls, identities, and
// PID membership only; destructive OOM and disk-fill probes belong to startup,
// identity-change conformance, and qualification. ADMISSION_ONLY facts never
// affect an already running allocation.
func periodicCapabilityAuditKeys(dependencies []*capabilityv1.CapabilityRequirement) []*capabilityv1.CapabilityKey {
	keys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY {
			continue
		}
		keys = append(keys, capabilitycontract.CloneKey(dependency.GetKey()))
	}
	return keys
}

func (h *sandboxService) runCapabilityReconcileWorker(ctx context.Context, allocationID string) {
	for {
		state := h.allocationController().CapabilityReconcileState(allocationID)
		if state == nil {
			return
		}
		if state.GetTerminating() {
			h.releaseCapabilityReconcileBudget(allocationID)
			h.startPendingCapabilityReconcileWorkers()
			h.failStopAllocation(ctx, allocationID, errors.New(state.GetLastError()))
			return
		}
		sequence := state.GetPendingObservationSequence()
		if sequence == 0 {
			return
		}
		if _, _, err := h.ReconcileAllocationCapabilities(ctx, allocationID); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Warn("retry capability reconcile work")
			if !waitCapabilityReconcileRetry(ctx) {
				return
			}
			continue
		}
		if current := h.allocationController().CapabilityReconcileState(allocationID); current != nil && current.GetTerminating() {
			continue
		}
		if err := h.allocationController().AckCapabilityReconcile(allocationID, sequence, false, nil); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("ack capability reconcile work")
			if !waitCapabilityReconcileRetry(ctx) {
				return
			}
		}
	}
}

func waitCapabilityReconcileRetry(ctx context.Context) bool {
	timer := time.NewTimer(capabilityReconcileRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *sandboxService) verifyFailStopCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityRequirement) (contract.CapabilityVerification, error) {
	results, err := h.verifyFailStopCapabilities(ctx, allocationID, []*capabilityv1.CapabilityRequirement{dependency})
	if err != nil {
		return contract.InconclusiveCapability(err), err
	}
	keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
	return results[keyID], nil
}

func (h *sandboxService) verifyFailStopCapabilities(ctx context.Context, allocationID string, dependencies []*capabilityv1.CapabilityRequirement) (map[string]contract.CapabilityVerification, error) {
	results, err := verifyCapabilityBatchWithDelays(ctx, inconclusiveVerificationDelays, len(dependencies), func(index int) contract.CapabilityVerification {
		return h.verifyAllocationCapability(ctx, allocationID, dependencies[index])
	})
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]contract.CapabilityVerification, len(dependencies))
	for index, dependency := range dependencies {
		keyID, keyErr := capabilitycontract.KeyID(dependency.GetKey())
		if keyErr != nil {
			return nil, keyErr
		}
		byKey[keyID] = results[index]
	}
	return byKey, nil
}

func verifyCapabilityWithDelays(ctx context.Context, delays []time.Duration, verify func() contract.CapabilityVerification) (contract.CapabilityVerification, error) {
	results, err := verifyCapabilityBatchWithDelays(ctx, delays, 1, func(int) contract.CapabilityVerification { return verify() })
	if err != nil {
		return contract.InconclusiveCapability(err), err
	}
	return results[0], nil
}

// verifyCapabilityBatchWithDelays retries only inconclusive verifications.
// Every pending capability is sampled once per round. A definitive loss ends
// the batch immediately after that round instead of waiting behind unrelated
// inconclusive capabilities, preserving the definition's fail-stop semantics.
func verifyCapabilityBatchWithDelays(ctx context.Context, delays []time.Duration, count int, verify func(int) contract.CapabilityVerification) ([]contract.CapabilityVerification, error) {
	if count < 0 || verify == nil || len(delays) == 0 {
		return nil, fmt.Errorf("capability verification count, verifier, and retry schedule are required")
	}
	for index, delay := range delays {
		if delay < 0 || (index > 0 && delay < delays[index-1]) {
			return nil, fmt.Errorf("capability verification retry schedule must be nonnegative and nondecreasing")
		}
	}
	results := make([]contract.CapabilityVerification, count)
	pending := make([]int, count)
	for index := range pending {
		pending[index] = index
	}
	for round, delay := range delays {
		if delay > 0 {
			previous := time.Duration(0)
			if round > 0 {
				previous = delays[round-1]
			}
			timer := time.NewTimer(delay - previous)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		nextPending := make([]int, 0, len(pending))
		definitiveLoss := false
		for _, index := range pending {
			results[index] = verify(index)
			if results[index].State == contract.CapabilityVerificationLost {
				definitiveLoss = true
			}
			if results[index].State == contract.CapabilityVerificationInconclusive && round+1 < len(delays) {
				nextPending = append(nextPending, index)
			}
		}
		if definitiveLoss || len(nextPending) == 0 {
			return results, nil
		}
		pending = nextPending
	}
	return results, nil
}

func (h *sandboxService) verifyAllocationCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityRequirement) contract.CapabilityVerification {
	platform := dependency.GetKey().GetPlatform()
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT {
		return verifyActiveEgressPolicy(ctx, h.egressClient, allocationID, h.allocationController().ContainerIP(allocationID), allocationNetworkPolicyMode([]*capabilityv1.CapabilityRequirement{dependency}))
	}
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET {
		manager := network.NetworkManagers[h.config.PluginConfig.NetworkConfig.NatBackend]
		prober, ok := manager.(network.HealthProber)
		if !ok {
			return contract.LostCapability(fmt.Errorf("network backend has no operational health verifier"))
		}
		health, err := prober.ProbeHealth(h.config.PluginConfig.NetworkConfig.IPRange)
		if err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("probe allocation dataplane: %w", err))
		}
		if !health.NativeDataplaneReady {
			return contract.LostCapability(fmt.Errorf("allocation network dataplane is not operational"))
		}
		networkState, err := h.NetworkForSandbox(allocationID)
		if err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("verify sandbox network resource: %w", err))
		}
		if _, err := os.Stat(networkState.NetNSPath); err != nil {
			wrapped := fmt.Errorf("verify sandbox network namespace: %w", err)
			if errors.Is(err, os.ErrNotExist) {
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
	handler := h.runscHandler
	if handler == nil {
		return contract.InconclusiveCapability(fmt.Errorf("runsc handler is unavailable"))
	}
	verifier, ok := handler.(contract.AllocationCapabilityVerifier)
	if !ok {
		return contract.LostCapability(fmt.Errorf("runsc has no allocation capability verifier"))
	}
	runtimeCgroupPath := ""
	memoryLimit := int64(0)
	ephemeralLimit := int64(0)
	manifest := h.allocationController().EnforcementManifest(allocationID)
	if manifest == nil {
		return contract.LostCapability(fmt.Errorf("durable allocation enforcement manifest is unavailable"))
	}
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT {
		runtimeCgroupPath, err = h.containerManager.RuntimeCgroupPath(allocationID)
		if err != nil {
			return contract.InconclusiveCapability(err)
		}
		if runtimeCgroupPath != manifest.GetRuntimeCgroupPath() {
			return contract.LostCapability(fmt.Errorf("runtime cgroup path differs from durable enforcement manifest"))
		}
		memoryLimit = manifest.GetMemoryLimitBytes()
	}
	ephemeralLimit = manifest.GetEphemeralStorageLimitBytes()
	return verifier.VerifyAllocationCapability(ctx, dependency, contract.HandlerOptions{
		ContainerID: allocationID, CgroupPath: manifest.GetCgroupPath(), RuntimeCgroupPath: runtimeCgroupPath,
		MemoryLimitBytes: memoryLimit, EphemeralStorageLimitBytes: ephemeralLimit,
		EnforcementManifest: manifest,
	})
}

func verificationMessage(verification contract.CapabilityVerification) string {
	if verification.Err == nil {
		return "capability enforcement could not be proven"
	}
	return verification.Err.Error()
}

func (h *sandboxService) failStopAllocation(ctx context.Context, allocationID string, verifyErr error) {
	runtimeName := config.RuntimeNameRunsc
	metrics.RecordCapabilityAllocationVerification(runtimeName, "fail_stop")
	// Emit before Delete removes allocation state. A successful fail-stop must
	// remain distinguishable from a workload-originated exit or kernel OOM.
	// Do not log verifyErr: verifier errors may contain policy destinations.
	logrus.WithFields(logrus.Fields{
		"allocation_id": allocationID, "runtime": runtimeName,
		"termination_owner": "capability_reconcile",
	}).Warn("allocation fail-stop initiated")
	for {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := h.allocationController().Delete(deleteCtx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 10})
		cancel()
		if err == nil {
			metrics.RecordCapabilityFailStopCleanup(runtimeName, "success")
			return
		}
		metrics.RecordCapabilityFailStopCleanup(runtimeName, "retry")
		_ = h.allocationController().AckCapabilityReconcile(allocationID, 0, true, errors.Join(verifyErr, err))
		logrus.WithError(err).WithField("allocation_id", allocationID).Error("retry fail-stop allocation cleanup")
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			metrics.RecordCapabilityFailStopCleanup(runtimeName, "worker_stopped")
			return
		case <-timer.C:
		}
	}
}

func (h *sandboxService) scheduleCapabilityTermination(allocationID string, cause error) error {
	if cause == nil {
		cause = errors.New("capability enforcement failed")
	}
	if err := h.allocationController().BeginCapabilityTermination(allocationID, cause); err != nil {
		return fmt.Errorf("%w; persist durable fail-stop: %v", cause, err)
	}
	h.startCapabilityReconcileWorker(allocationID)
	return cause
}

// ReconcileAllocationCapabilities builds a fresh full diagnostic projection
// from the immutable requirements, current Node observation, and runtime.
func (h *sandboxService) ReconcileAllocationCapabilities(ctx context.Context, allocationID string) ([]*capabilityv1.CapabilityRequirement, *capabilityv1.CapabilityConditionSet, error) {
	dependencies := h.allocationController().CapabilityRequirementManifests()[strings.TrimSpace(allocationID)]
	if len(dependencies) == 0 {
		set := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.Now()}
		h.controlPlaneReports.ReportCapabilityConditions(allocationID, set)
		return nil, set, nil
	}
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(dependencies))
	now := time.Now().UTC()
	snapshot := h.capabilityManager.Snapshot()
	failStopDependencies := make([]*capabilityv1.CapabilityRequirement, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			failStopDependencies = append(failStopDependencies, dependency)
		}
	}
	failStopVerifications, err := h.verifyFailStopCapabilities(ctx, allocationID, failStopDependencies)
	if err != nil {
		return nil, nil, err
	}
	definitiveFailStopLoss := false
	for _, verification := range failStopVerifications {
		if verification.State == contract.CapabilityVerificationLost {
			definitiveFailStopLoss = true
			break
		}
	}
	for _, dependency := range dependencies {
		_, available := capabilitycontract.AvailableObservation(snapshot, dependency.GetKey(), now)
		state := capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY
		code := capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
		message := "allocation capability remains valid"
		if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY {
			verification := contract.CapabilityVerification{}
			if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
				keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
				verification = failStopVerifications[keyID]
			} else {
				verification = h.verifyAllocationCapability(ctx, allocationID, dependency)
			}
			if verification.State != contract.CapabilityVerificationVerified {
				state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
				code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST
				message = "allocation-specific capability verification is degraded: " + verificationMessage(verification)
				if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
					if definitiveFailStopLoss && verification.State == contract.CapabilityVerificationInconclusive {
						state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_UNKNOWN
						code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR
						message = "verification stopped after another hard capability definitively failed"
					} else {
						state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED
						message = "CAPABILITY_ENFORCEMENT_LOST: " + verificationMessage(verification)
					}
				}
			} else if !available {
				state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
				code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE
				message = "node observation is unavailable, but allocation-specific enforcement remains valid"
			} else {
				message = "allocation-specific enforcement remains valid"
			}
		} else if !available {
			state = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
			code = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE
			message = "admission-only capability observation is no longer available; running allocation is unchanged"
		}
		condition := &capabilityv1.CapabilityCondition{Key: capabilitycontract.CloneKey(dependency.GetKey()), State: state, ReasonCode: code, Message: capabilitycontract.BoundedReason(message)}
		conditions = append(conditions, condition)
	}
	set, err := h.allocationController().ReplaceCapabilityConditions(allocationID, conditions, now)
	if err != nil {
		return nil, nil, err
	}
	h.controlPlaneReports.ReportCapabilityConditions(allocationID, set)
	var terminationReasons []error
	for _, condition := range conditions {
		if condition.GetState() != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED {
			continue
		}
		terminationReasons = append(terminationReasons, fmt.Errorf("%s: %s", capabilitycontract.MetricKey(condition.GetKey()), condition.GetMessage()))
	}
	if len(terminationReasons) > 0 {
		if err := h.allocationController().BeginCapabilityTermination(allocationID, errors.Join(terminationReasons...)); err != nil {
			return nil, nil, fmt.Errorf("persist capability fail-stop ownership: %w", err)
		}
		h.startCapabilityReconcileWorker(allocationID)
	}
	return dependencies, set, nil
}
