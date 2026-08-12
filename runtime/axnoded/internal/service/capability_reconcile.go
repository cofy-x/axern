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
	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/network"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
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

type capabilityReconcileResult struct {
	retryErr         error
	terminationCause error
}

func (h *sandboxService) handleCapabilityTransitions(_ context.Context, transitions []*nodecapability.Transition) {
	if h == nil || len(transitions) == 0 {
		return
	}
	manifests := h.allocationController().CapabilityDependencyManifests()
	for allocationID, dependencies := range manifests {
		keys := make([]*capabilityv1.CapabilityKey, 0, len(transitions))
		generation := int64(0)
		for _, transition := range transitions {
			dependency := matchingDependency(dependencies, transition.Key)
			if dependency == nil || dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY {
				continue
			}
			keys = append(keys, capabilitycontract.CloneKey(transition.Key))
			if transition.Generation > generation {
				generation = transition.Generation
			}
		}
		if len(keys) == 0 {
			continue
		}
		if err := h.allocationController().MergeCapabilityReconcile(allocationID, generation, keys); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("persist capability reconcile work")
			continue
		}
		h.startCapabilityReconcileWorker(allocationID)
	}
}

// startCapabilityReconcileWorker establishes allocation-level single ownership.
// New generations are persisted before this call and are therefore picked up
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
	manifests := h.allocationController().CapabilityDependencyManifests()
	allocationIDs := make([]string, 0, len(manifests))
	for allocationID := range manifests {
		allocationIDs = append(allocationIDs, allocationID)
	}
	sort.Strings(allocationIDs)
	for _, allocationID := range allocationIDs {
		state := h.allocationController().CapabilityReconcileState(allocationID)
		if state == nil || (!state.GetTerminating() && len(state.GetPending()) == 0) {
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
			var now time.Time
			select {
			case <-ctx.Done():
				return
			case now = <-ticker.C:
			}
			h.startPendingCapabilityReconcileWorkers()
			currentAuditShard := auditShard
			auditShard = nextCapabilityAuditShard(auditShard)
			snapshot := h.capabilityManager.Snapshot()
			generation := snapshot.GetSequence()
			if generation <= 0 {
				generation = now.UTC().UnixNano()
			}
			for allocationID, dependencies := range h.allocationController().CapabilityDependencyManifests() {
				if capabilityAuditShard(allocationID) != currentAuditShard {
					continue
				}
				keys := periodicCapabilityAuditKeys(dependencies)
				if len(keys) == 0 {
					continue
				}
				if err := h.allocationController().MergeCapabilityReconcile(allocationID, generation, keys); err != nil {
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
func periodicCapabilityAuditKeys(dependencies []*capabilityv1.CapabilityDependency) []*capabilityv1.CapabilityKey {
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
		if len(state.GetPending()) == 0 {
			return
		}
		pending := make([]*runtimev1.PendingCapabilityReconcile, 0, len(state.GetPending()))
		for _, item := range state.GetPending() {
			pending = append(pending, proto.Clone(item).(*runtimev1.PendingCapabilityReconcile))
		}
		dependencies := h.allocationController().CapabilityDependencyManifests()[allocationID]
		terminateReasons := make([]error, 0)
		retryReasons := make([]error, 0)
		failStopDependencies := make([]*capabilityv1.CapabilityDependency, 0, len(pending))
		for _, item := range pending {
			dependency := matchingDependency(dependencies, item.GetKey())
			if dependency == nil {
				keyID, _ := capabilitycontract.KeyID(item.GetKey())
				terminateReasons = append(terminateReasons, fmt.Errorf("CAPABILITY_ENFORCEMENT_LOST: pending capability %q has no durable allocation dependency", keyID))
				continue
			}
			if dependency.GetLossPolicy() == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
				failStopDependencies = append(failStopDependencies, dependency)
				continue
			}
			result := h.reconcileDegradeCapability(ctx, allocationID, dependency)
			if result.terminationCause != nil {
				terminateReasons = append(terminateReasons, result.terminationCause)
			}
			if result.retryErr != nil {
				retryReasons = append(retryReasons, result.retryErr)
			}
		}
		if len(terminateReasons) == 0 && len(failStopDependencies) > 0 {
			verifications, verifyErr := h.verifyFailStopCapabilities(ctx, allocationID, failStopDependencies)
			if verifyErr != nil {
				retryReasons = append(retryReasons, fmt.Errorf("capability verification interrupted: %w", verifyErr))
			} else {
				definitiveLoss := false
				for _, verification := range verifications {
					if verification.State == contract.CapabilityVerificationLost {
						definitiveLoss = true
						break
					}
				}
				for _, dependency := range failStopDependencies {
					keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
					verification := verifications[keyID]
					if verification.State == contract.CapabilityVerificationVerified {
						if reportErr := h.reportVerifiedCapabilityCondition(allocationID, dependency, "allocation-specific enforcement remains valid"); reportErr != nil {
							retryReasons = append(retryReasons, reportErr)
						}
						continue
					}
					if definitiveLoss && verification.State == contract.CapabilityVerificationInconclusive {
						// Another hard capability already proved unsafe in this
						// round. Do not misreport an unrelated, unexhausted probe
						// as a second enforcement loss while termination begins.
						continue
					}
					message := "CAPABILITY_ENFORCEMENT_LOST: " + verificationMessage(verification)
					conditionErr := h.reportCapabilityCondition(allocationID, dependency, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST, message)
					terminateReasons = append(terminateReasons, errors.Join(errors.New(message), conditionErr))
				}
			}
		}
		if len(terminateReasons) > 0 {
			reason := errors.Join(terminateReasons...)
			if err := h.allocationController().AckCapabilityReconcile(allocationID, nil, true, reason); err != nil {
				logrus.WithError(err).WithField("allocation_id", allocationID).Error("persist fail-stop ownership")
				return
			}
			h.releaseCapabilityReconcileBudget(allocationID)
			h.startPendingCapabilityReconcileWorkers()
			h.failStopAllocation(ctx, allocationID, reason)
			return
		}
		if len(retryReasons) > 0 {
			logrus.WithError(errors.Join(retryReasons...)).WithField("allocation_id", allocationID).Warn("retry capability reconcile work")
			if !waitCapabilityReconcileRetry(ctx) {
				return
			}
			continue
		}
		if err := h.allocationController().AckCapabilityReconcile(allocationID, pending, false, nil); err != nil {
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

// reconcileAllocationCapability returns a non-nil error only when the
// allocation must enter the shared fail-stop termination workflow.
func (h *sandboxService) reconcileDegradeCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency) capabilityReconcileResult {
	if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE {
		return capabilityReconcileResult{terminationCause: fmt.Errorf("CAPABILITY_ENFORCEMENT_LOST: unexpected queued loss policy %s", dependency.GetLossPolicy())}
	}
	verification := h.verifyAllocationCapability(ctx, allocationID, dependency)
	if verification.State == contract.CapabilityVerificationVerified {
		return capabilityReconcileResult{retryErr: h.reportVerifiedCapabilityCondition(allocationID, dependency, "allocation-specific dataplane remains operational")}
	}
	return capabilityReconcileResult{retryErr: h.reportCapabilityCondition(allocationID, dependency, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST, "node capability is degraded: "+verificationMessage(verification))}
}

func (h *sandboxService) verifyFailStopCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency) (contract.CapabilityVerification, error) {
	results, err := h.verifyFailStopCapabilities(ctx, allocationID, []*capabilityv1.CapabilityDependency{dependency})
	if err != nil {
		return contract.InconclusiveCapability(err), err
	}
	keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
	return results[keyID], nil
}

func (h *sandboxService) verifyFailStopCapabilities(ctx context.Context, allocationID string, dependencies []*capabilityv1.CapabilityDependency) (map[string]contract.CapabilityVerification, error) {
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
// inconclusive capabilities, preserving the catalog's fail-stop semantics.
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

func (h *sandboxService) verifyAllocationCapability(ctx context.Context, allocationID string, dependency *capabilityv1.CapabilityDependency) contract.CapabilityVerification {
	platform := dependency.GetKey().GetPlatform()
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET {
		manager := network.NetworkManagers[h.config.PluginConfig.NetworkConfig.NatBackend]
		prober, ok := manager.(network.HealthProber)
		if !ok {
			return contract.LostCapability(fmt.Errorf("network backend has no operational health verifier"))
		}
		health, err := prober.ProbeHealth(h.config.PluginConfig.NetworkConfig.IPRange)
		if err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("probe allocation dataplane: %w", err))
		}
		if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING {
			if !health.PortForwardingReady || len(h.sandboxNetworking().DnatRules(allocationID)) == 0 {
				return contract.LostCapability(fmt.Errorf("allocation port-forwarding dataplane is not operational"))
			}
			return contract.VerifiedCapability()
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
	handler, ok := h.runtimeHandlers.Get(ct.Metadata.GetRuntimeHandler())
	if !ok {
		return contract.InconclusiveCapability(fmt.Errorf("runtime handler %q is unavailable", ct.Metadata.GetRuntimeHandler()))
	}
	verifier, ok := handler.(contract.AllocationCapabilityVerifier)
	if !ok {
		return contract.LostCapability(fmt.Errorf("runtime %q has no allocation capability verifier", handler.Name()))
	}
	runtimeCgroupPath := ""
	memoryLimit := int64(0)
	ephemeralLimit := int64(0)
	manifest := h.allocationController().EnforcementManifest(allocationID)
	if manifest == nil {
		return contract.LostCapability(fmt.Errorf("durable allocation enforcement manifest is unavailable"))
	}
	if platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT {
		runtimeCgroupPath, err = h.containerManager.RuntimeCgroupPath(allocationID)
		if err != nil {
			return contract.InconclusiveCapability(err)
		}
		if runtimeCgroupPath != manifest.GetRuntimeCgroupPath() {
			return contract.LostCapability(fmt.Errorf("runtime cgroup path differs from durable enforcement manifest"))
		}
	}
	if ct.Status != nil {
		status := ct.Status.Get()
		if status.LinuxResources != nil {
			memoryLimit = status.LinuxResources.GetMemoryLimitInBytes()
		}
	}
	if (platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT || platform == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT) && memoryLimit != manifest.GetMemoryLimitBytes() {
		return contract.LostCapability(fmt.Errorf("container memory limit differs from durable enforcement manifest"))
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
	runtimeName := "unknown"
	if ct, err := h.containerManager.Get(allocationID); err == nil && ct != nil && ct.Metadata != nil {
		runtimeName = ct.Metadata.GetRuntimeHandler()
	}
	metrics.RecordCapabilityAllocationVerification(runtimeName, "fail_stop")
	for {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := h.allocationController().Delete(deleteCtx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 10})
		cancel()
		if err == nil {
			metrics.RecordCapabilityFailStopCleanup(runtimeName, "success")
			return
		}
		metrics.RecordCapabilityFailStopCleanup(runtimeName, "retry")
		_ = h.allocationController().AckCapabilityReconcile(allocationID, nil, true, errors.Join(verifyErr, err))
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

func (h *sandboxService) reportCapabilityCondition(allocationID string, dependency *capabilityv1.CapabilityDependency, state capabilityv1.CapabilityConditionState, reasonCode capabilityv1.CapabilityReasonCode, message string) error {
	attempt, found := h.allocationController().ManagedAllocationAttempt(allocationID)
	now := time.Now().UTC()
	proof := dependency.GetSelectedObservation()
	if snapshot := h.capabilityManager.Snapshot(); snapshot != nil {
		if observation, ok := capabilitycontract.AvailableObservation(snapshot, dependency.GetKey(), now); ok {
			proof = capabilitycontract.NewObservationProof(observation)
		}
	}
	condition := &capabilityv1.CapabilityCondition{
		Key: capabilitycontract.CloneKey(dependency.GetKey()), State: state, ReasonCode: reasonCode,
		Message: capabilitycontract.BoundedReason(strings.TrimSpace(message)), ObservedAt: timestamppb.New(now),
	}
	if proof != nil {
		condition.Proof = proto.Clone(proof).(*capabilityv1.CapabilityObservationProof)
	}
	conditionSet, err := h.allocationController().UpdateCapabilityCondition(allocationID, condition, now)
	if err != nil {
		return fmt.Errorf("persist allocation capability condition: %w", err)
	}
	if found {
		h.controlPlaneReports.ReportCapabilityConditions(allocationID, attempt, conditionSet)
	}
	return nil
}

func (h *sandboxService) reportVerifiedCapabilityCondition(allocationID string, dependency *capabilityv1.CapabilityDependency, message string) error {
	now := time.Now().UTC()
	state, reasonCode := verifiedCapabilityCondition(h.capabilityManager.Snapshot(), dependency.GetKey(), now)
	if state == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY {
		return h.reportCapabilityCondition(allocationID, dependency, state, reasonCode, message)
	}
	return h.reportCapabilityCondition(allocationID, dependency, state, reasonCode, message+"; node-wide observation is unavailable")
}

// verifiedCapabilityCondition keeps node evidence and allocation enforcement
// as independent proofs. A successful allocation verifier cannot refresh or
// replace an expired node observation, so the allocation remains degraded
// until the provider publishes current evidence again.
func verifiedCapabilityCondition(snapshot *capabilityv1.CapabilitySnapshot, key *capabilityv1.CapabilityKey, now time.Time) (capabilityv1.CapabilityConditionState, capabilityv1.CapabilityReasonCode) {
	if _, available := capabilitycontract.AvailableObservation(snapshot, key, now); available {
		return capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE
	}
	return capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED, capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE
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

// ReconcileAllocationCapabilities is the controld safety-net entrypoint. It
// writes through the same durable full-set condition path as local transitions.
func (h *sandboxService) ReconcileAllocationCapabilities(ctx context.Context, allocationID string) ([]*capabilityv1.CapabilityDependency, *capabilityv1.CapabilityConditionSet, error) {
	dependencies := h.allocationController().CapabilityDependencyManifests()[strings.TrimSpace(allocationID)]
	if len(dependencies) == 0 {
		return nil, h.allocationController().CapabilityConditions(allocationID), nil
	}
	attempt, found := h.allocationController().ManagedAllocationAttempt(allocationID)
	if !found {
		return nil, nil, fmt.Errorf("managed allocation %q has capability dependencies but no durable attempt", allocationID)
	}
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(dependencies))
	now := time.Now().UTC()
	snapshot := h.capabilityManager.Snapshot()
	failStopDependencies := make([]*capabilityv1.CapabilityDependency, 0, len(dependencies))
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
		observation, available := capabilitycontract.AvailableObservation(snapshot, dependency.GetKey(), now)
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
		proof := dependency.GetSelectedObservation()
		if observation != nil {
			proof = capabilitycontract.NewObservationProof(observation)
		}
		condition := &capabilityv1.CapabilityCondition{Key: capabilitycontract.CloneKey(dependency.GetKey()), State: state, ReasonCode: code, Message: capabilitycontract.BoundedReason(message), ObservedAt: timestamppb.New(now)}
		if proof != nil {
			condition.Proof = proto.Clone(proof).(*capabilityv1.CapabilityObservationProof)
		}
		conditions = append(conditions, condition)
	}
	set, err := h.allocationController().ReplaceCapabilityConditions(allocationID, conditions, now)
	if err != nil {
		return nil, nil, err
	}
	h.controlPlaneReports.ReportCapabilityConditions(allocationID, attempt, set)
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
