package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

func (h *sandboxService) Start(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	spanAttrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, request.GetContainerID()),
		attribute.String(sdkobs.AttrRuntime, request.GetRuntimeTemplate().GetSandbox()),
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanAllocationStart,
		SpanAttrs:   spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrRuntime, request.GetRuntimeTemplate().GetSandbox())},
		Counter:     sandboxobs.MetricAllocationStartTotal,
		Duration:    sandboxobs.MetricAllocationStartDuration,
	})
	var err error
	defer func() { op.End(err) }()
	requestDigest, err := allocation.StartRequestDigest(request)
	if err != nil {
		return nil, fmt.Errorf("identify allocation request: %w", err)
	}
	controller := h.allocationController()
	unlockLifecycle := controller.LockAllocationLifecycle(request.GetContainerID())
	defer unlockLifecycle()
	if controller.LaunchVerification(request.GetContainerID()) != nil {
		if controller.AllocationRequestDigest(request.GetContainerID()) != requestDigest {
			return nil, errord.ToGRPC(fmt.Errorf("allocation request differs from the durable contract for this attempt: %w", errord.ErrFailedPrecondition))
		}
		resp, active, replayErr := controller.ExistingActiveStartResponseWithLifecycleHeld(ctx, request)
		if replayErr != nil {
			return resp, errord.ToGRPC(replayErr)
		}
		if !active {
			return nil, errord.ToGRPC(fmt.Errorf("durably verified allocation has no active runtime: %w", errord.ErrFailedPrecondition))
		}
		resp.AdmittedCapabilityDependencies = controller.CapabilityDependencies(request.GetContainerID())
		resp.CapabilityVerification = controller.CapabilityAdmissionConditions(request.GetContainerID())
		if resp.CapabilityVerification == nil {
			return nil, errord.ToGRPC(fmt.Errorf("durably verified allocation is missing its sealed create proof: %w", errord.ErrFailedPrecondition))
		}
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "replayed")
		return resp, nil
	}
	// A live same-attempt replay is defined by the immutable request digest and
	// sealed create proof above. Current node policy may legitimately differ
	// after a config or runtime identity change; applying it retroactively would
	// break idempotency. New creates still derive and verify the complete current
	// requirement contract before any allocation side effect.
	if err = h.verifyRequestCapabilityRequirements(request); err != nil {
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "requirement_mismatch")
		op.SetErrorStatus("allocation capability requirements do not match request")
		return nil, fmt.Errorf("derive allocation capability requirements: %w", err)
	}
	preCreateObservedAt := time.Now().UTC()
	admitted, verification, err := h.admitCapabilityDependencies(request.GetCapabilityDependencies(), preCreateObservedAt)
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "pre_create_failed")
		op.SetErrorStatus("allocation capability gate failed")
		return nil, fmt.Errorf("verify allocation capabilities before create: %w", err)
	}
	nodeLocal := allocation.IsNodeLocalStart(ctx)
	if request.GetAllocationAttempt() > 0 || nodeLocal {
		if nodeLocal {
			_, err = controller.ReplaceNodeLocalCapabilityAdmission(request.GetContainerID(), requestDigest, admitted, verification, preCreateObservedAt)
		} else {
			_, err = controller.ReplaceCapabilityAdmission(request.GetContainerID(), request.GetAllocationAttempt(), requestDigest, admitted, verification, preCreateObservedAt)
		}
		if err != nil {
			op.SetErrorStatus("persist allocation capability admission failed")
			return nil, err
		}
	}
	resp, err := controller.StartWithLifecycleHeld(ctx, request)
	if err != nil || resp == nil || resp.GetCode() != 0 {
		if err == nil {
			if resp == nil {
				err = fmt.Errorf("allocation start returned no response")
			} else {
				err = fmt.Errorf("allocation start failed: %s", resp.GetMessage())
			}
		}
		if cleanupErr := h.cleanupFailedStartDetached(request.GetContainerID()); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup failed-start allocation: %v", err, cleanupErr)
		}
		op.SetErrorStatus("allocation start failed")
		return resp, errord.ToGRPC(err)
	}
	admitted, verification, err = h.verifyPostCreateCapabilityDependencies(ctx, request.GetContainerID(), request.GetCapabilityDependencies(), time.Now())
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "post_create_failed")
		err = h.scheduleCapabilityTermination(request.GetContainerID(), fmt.Errorf("verify allocation capabilities after create: %w", err))
		op.SetErrorStatus("post-create capability enforcement failed")
		return nil, err
	}
	var conditionSet *capabilityv1.CapabilityConditionSet
	if request.GetAllocationAttempt() > 0 || nodeLocal {
		if nodeLocal {
			conditionSet, err = h.allocationController().ReplaceNodeLocalCapabilityAdmission(request.GetContainerID(), requestDigest, admitted, verification, time.Now().UTC())
		} else {
			conditionSet, err = h.allocationController().ReplaceCapabilityAdmission(request.GetContainerID(), request.GetAllocationAttempt(), requestDigest, admitted, verification, time.Now().UTC())
		}
		if err != nil {
			return nil, h.scheduleCapabilityTermination(request.GetContainerID(), fmt.Errorf("persist post-create capability admission: %w", err))
		}
	}
	resp.CapabilityVerification = conditionSet
	resp.AdmittedCapabilityDependencies = admitted
	if !nodeLocal {
		h.controlPlaneReports.ReportCapabilityConditions(request.GetContainerID(), request.GetAllocationAttempt(), conditionSet)
	}
	metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "verified")
	return resp, nil
}

// StartNodeLocalSandbox is the in-process operator path used by node-owned
// diagnostics. It accepts only a materialized local rootfs, derives the exact
// workload requirements from node configuration and actual backing facts, and
// binds them to the manager's current observation proofs before entering the
// ordinary Start gates. There is deliberately no protobuf/RPC switch for this
// path.
func (h *sandboxService) StartNodeLocalSandbox(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	prepared, err := h.prepareNodeLocalStartRequest(request, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("prepare node-local capability admission: %w", err)
	}
	return h.Start(allocation.WithNodeLocalStart(ctx), prepared)
}

func (h *sandboxService) prepareNodeLocalStartRequest(request *runtime.StartRequest, now time.Time) (*runtime.StartRequest, error) {
	if request == nil || request.GetRuntimeTemplate() == nil || request.GetRuntimeTemplate().GetRootfs() == nil {
		return nil, fmt.Errorf("runtime template and rootfs are required")
	}
	if request.GetAllocationAttempt() != 0 {
		return nil, fmt.Errorf("node-local sandbox cannot declare a control-plane allocation attempt")
	}
	if len(request.GetCapabilityDependencies()) != 0 {
		return nil, fmt.Errorf("node-local sandbox cannot supply capability dependencies")
	}
	rootfs := request.GetRuntimeTemplate().GetRootfs()
	if rootfs.GetType() != runtime.RootfsSrcType_LOCAL || strings.TrimSpace(rootfs.GetPath()) == "" {
		return nil, fmt.Errorf("node-local sandbox requires a materialized local rootfs")
	}
	if h.capabilityManager == nil || !h.capabilityManager.Ready() {
		return nil, fmt.Errorf("capability manager is warming")
	}
	facts, err := rootfsview.InspectBacking(rootfs.GetPath())
	if err != nil {
		return nil, fmt.Errorf("inspect node-local rootfs backing: %w", err)
	}
	keys, err := capabilitycontract.DeriveRequirements(h.requirementInput(request, facts.HasFilesystem("erofs")))
	if err != nil {
		return nil, fmt.Errorf("derive node-local capability requirements: %w", err)
	}
	snapshot := h.capabilityManager.Snapshot()
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, keys, now)
	if err != nil {
		return nil, fmt.Errorf("resolve node-local capability requirements: %w", err)
	}
	prepared := proto.Clone(request).(*runtime.StartRequest)
	prepared.CapabilityDependencies = dependencies
	return prepared, nil
}

func (h *sandboxService) ManagedAllocationAttempt(allocationID string) (int64, bool) {
	return h.allocationController().ManagedAllocationAttempt(allocationID)
}

func (h *sandboxService) verifyPreparedAllocationCapabilities(ctx context.Context, request *runtime.StartRequest, handler contract.ManagedRuntimeHandler, containerID string) error {
	if request == nil || handler == nil {
		return fmt.Errorf("managed allocation request and runtime handler are required")
	}
	if request.GetContainerID() != "" && request.GetContainerID() != containerID {
		return fmt.Errorf("prepared runtime identity %q differs from allocation identity %q", containerID, request.GetContainerID())
	}
	manifest, err := handler.AllocationEnforcementManifest(ctx, containerID)
	if err != nil {
		return fmt.Errorf("read immutable runtime enforcement manifest: %w", err)
	}
	durableDependencies := h.allocationController().CapabilityDependencies(containerID)
	dependencies := request.GetCapabilityDependencies()
	if allocation.IsInternalConformance(ctx) {
		keys, deriveErr := capabilitycontract.DeriveRequirements(h.requirementInput(request, false))
		if deriveErr != nil {
			return fmt.Errorf("derive internal conformance requirements: %w", deriveErr)
		}
		dependencies = make([]*capabilityv1.CapabilityDependency, 0, len(keys))
		for _, key := range keys {
			definition, ok := capabilitycontract.PlatformDefinition(key.GetPlatform())
			if key.GetExtension() != nil || !ok || definition.LossPolicy != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
				continue
			}
			dependencies = append(dependencies, &capabilityv1.CapabilityDependency{Key: capabilitycontract.CloneKey(key), LossPolicy: definition.LossPolicy})
		}
	} else {
		currentDependencies, _, admitErr := h.admitCapabilityDependencies(dependencies, time.Now().UTC())
		if admitErr != nil {
			return fmt.Errorf("revalidate current pre-activation capability proof: %w", admitErr)
		}
		durableKeys, durableErr := dependencyKeys(durableDependencies, false)
		if durableErr != nil {
			return fmt.Errorf("validate durable pre-create capability dependencies: %w", durableErr)
		}
		currentKeys, currentErr := dependencyKeys(currentDependencies, false)
		if currentErr != nil {
			return fmt.Errorf("validate rootfs-gated capability dependencies: %w", currentErr)
		}
		if !capabilitycontract.RequirementKeysEqual(durableKeys, currentKeys) {
			return fmt.Errorf("rootfs-gated capability requirements differ from durable pre-create admission")
		}
		request.CapabilityDependencies = currentDependencies
		dependencies = currentDependencies
	}

	var verifier contract.AllocationCapabilityVerifier
	verifiedKeys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			continue
		}
		if verifier == nil {
			var ok bool
			verifier, ok = handler.(contract.AllocationCapabilityVerifier)
			if !ok {
				return fmt.Errorf("runtime %q has no allocation capability verifier", handler.Name())
			}
		}
		verification := verifier.VerifyAllocationCapability(ctx, dependency, contract.HandlerOptions{
			ContainerID: containerID,
			CgroupPath:  manifest.GetCgroupPath(), RuntimeCgroupPath: manifest.GetRuntimeCgroupPath(),
			MemoryLimitBytes: manifest.GetMemoryLimitBytes(), EphemeralStorageLimitBytes: manifest.GetEphemeralStorageLimitBytes(),
			EnforcementManifest: manifest,
		})
		if verification.State != contract.CapabilityVerificationVerified {
			return fmt.Errorf("verify %s before workload start: %s", capabilitycontract.MetricKey(dependency.GetKey()), verificationMessage(verification))
		}
		verifiedKeys = append(verifiedKeys, capabilitycontract.CloneKey(dependency.GetKey()))
	}
	return h.allocationController().StoreLaunchVerification(containerID, manifest, verifiedKeys, time.Now().UTC())
}

func (h *sandboxService) cleanupFailedStartDetached(allocationID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return h.allocationController().CleanupFailedStart(ctx, allocationID)
}

func (h *sandboxService) requirementInput(request *runtime.StartRequest, erofs bool) capabilitycontract.RequirementInput {
	resources := request.GetResources()
	template := request.GetRuntimeTemplate()
	return capabilitycontract.RequirementInput{
		RuntimeName:                 template.GetSandbox(),
		HasPorts:                    len(request.GetPorts()) > 0,
		NetworkMode:                 request.GetNetwork(),
		NetworkBackend:              h.config.PluginConfig.NetworkConfig.NatBackend,
		MemoryLimitBytes:            resources.GetLimits().GetMemoryBytes(),
		RootfsWritable:              !template.GetRootfs().GetReadonly(),
		EphemeralStorageLimitBytes:  resources.GetLimits().GetEphemeralStorageBytes(),
		EROFSBacking:                erofs,
		ExtensionCapabilityRequests: request.GetExtensionCapabilityRequirements(),
	}
}

func dependencyKeys(dependencies []*capabilityv1.CapabilityDependency, excludeEROFS bool) ([]*capabilityv1.CapabilityKey, error) {
	keys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency == nil {
			return nil, fmt.Errorf("capability dependency is required")
		}
		if excludeEROFS && dependency.GetKey().GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS {
			continue
		}
		keys = append(keys, capabilitycontract.CloneKey(dependency.GetKey()))
	}
	if err := capabilitycontract.ValidateRequirementKeys(keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (h *sandboxService) verifyRequestCapabilityRequirements(request *runtime.StartRequest) error {
	if request == nil || request.GetRuntimeTemplate() == nil || request.GetRuntimeTemplate().GetRootfs() == nil {
		return fmt.Errorf("runtime template and rootfs are required")
	}
	if strings.TrimSpace(request.GetContainerID()) == "" {
		return fmt.Errorf("allocation id is required")
	}
	derived, err := capabilitycontract.DeriveRequirements(h.requirementInput(request, false))
	if err != nil {
		return err
	}
	supplied, err := dependencyKeys(request.GetCapabilityDependencies(), true)
	if err != nil {
		return fmt.Errorf("validate supplied dependencies: %w", err)
	}
	if !capabilitycontract.RequirementKeysEqual(derived, supplied) {
		return fmt.Errorf("supplied dependencies do not exactly match request-derived requirements")
	}
	return nil
}

func (h *sandboxService) verifyRootfsCapabilityRequirements(ctx context.Context, request *runtime.StartRequest, rootfsPath string) error {
	if allocation.IsInternalConformance(ctx) {
		return nil
	}
	facts, err := rootfsview.InspectBacking(rootfsPath)
	if err != nil {
		return fmt.Errorf("inspect rootfs backing: %w", err)
	}
	derived, err := capabilitycontract.DeriveRequirements(h.requirementInput(request, facts.HasFilesystem("erofs")))
	if err != nil {
		return err
	}
	supplied, err := dependencyKeys(request.GetCapabilityDependencies(), false)
	if err != nil {
		return fmt.Errorf("validate supplied dependencies: %w", err)
	}
	if !capabilitycontract.RequirementKeysEqual(derived, supplied) {
		return fmt.Errorf("supplied dependencies do not exactly match actual rootfs-derived requirements (filesystem=%s)", facts.FSType)
	}
	// Rootfs materialization may take longer than a health observation's TTL.
	// Rebind the exact requirement set to the manager's current snapshot before
	// bundle, filestore, cgroup, or runtime state is created. The request digest
	// intentionally excludes observation identity, so replacing placement
	// evidence here does not change the allocation's immutable workload contract.
	admitted, _, err := h.admitCapabilityDependencies(request.GetCapabilityDependencies(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("revalidate capabilities after rootfs materialization: %w", err)
	}
	request.CapabilityDependencies = admitted
	return nil
}

func (h *sandboxService) verifyPostCreateCapabilityDependencies(ctx context.Context, containerID string, dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	admitted, conditions, err := h.admitCapabilityDependencies(dependencies, now)
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[string]*capabilityv1.CapabilityCondition, len(conditions))
	for _, condition := range conditions {
		id, keyErr := capabilitycontract.KeyID(condition.GetKey())
		if keyErr != nil {
			return nil, nil, keyErr
		}
		byKey[id] = condition
	}
	launchVerification := h.allocationController().LaunchVerification(containerID)
	verifiedAtLaunch := make(map[string]struct{})
	if launchVerification != nil {
		for _, key := range launchVerification.GetVerifiedCapabilities() {
			id, keyErr := capabilitycontract.KeyID(key)
			if keyErr != nil {
				return nil, nil, fmt.Errorf("validate persisted launch verification: %w", keyErr)
			}
			if _, duplicate := verifiedAtLaunch[id]; duplicate {
				return nil, nil, fmt.Errorf("persisted launch verification contains duplicate capability %q", id)
			}
			verifiedAtLaunch[id] = struct{}{}
		}
	}
	for _, dependency := range admitted {
		if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			continue
		}
		id, keyErr := capabilitycontract.KeyID(dependency.GetKey())
		if keyErr != nil {
			return nil, nil, keyErr
		}
		if _, ok := verifiedAtLaunch[id]; !ok {
			return nil, nil, fmt.Errorf("capability %q has no durable create-before-start verification", id)
		}
		delete(verifiedAtLaunch, id)
		if condition := byKey[id]; condition != nil {
			condition.Message = "runtime-specific enforcement verified before workload start"
		}
	}
	if len(verifiedAtLaunch) != 0 {
		return nil, nil, fmt.Errorf("launch verification contains capabilities outside the admitted dependency set")
	}
	return admitted, conditions, nil
}

func (h *sandboxService) admitCapabilityDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	if len(dependencies) == 0 {
		return nil, nil, nil
	}
	if h.capabilityManager == nil {
		return nil, nil, fmt.Errorf("capability manager is unavailable")
	}
	return h.capabilityManager.AdmitDependencies(dependencies, now)
}

func (h *sandboxService) Delete(ctx context.Context, request *runtime.DeleteRequest) (response *runtime.DeleteResponse, err error) {
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: sandboxobs.SpanAllocationDelete,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrAllocationID, request.GetID()),
		},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "delete")},
		Counter:     sandboxobs.MetricAllocationDeleteTotal,
		Duration:    sandboxobs.MetricAllocationDeleteDuration,
	})
	defer func() { op.End(err) }()
	resp, err := h.allocationController().Delete(ctx, request)
	if err != nil {
		op.SetErrorStatus("allocation delete failed")
		return resp, errord.ToGRPC(err)
	}
	return resp, nil
}
