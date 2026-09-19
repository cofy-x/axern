package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (h *sandboxService) Start(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	return h.start(ctx, request, "")
}

func (h *sandboxService) start(ctx context.Context, request *runtime.StartRequest, controlPlaneNodeID string) (*runtime.StartResponse, error) {
	if err := startplan.ValidateStartRequest(request); err != nil {
		return nil, errord.ToGRPC(err)
	}
	spanAttrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, request.GetAllocationID()),
		attribute.String(sdkobs.AttrRuntime, config.RuntimeNameRunsc),
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanAllocationStart,
		SpanAttrs:   spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrRuntime, config.RuntimeNameRunsc)},
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
	leaseTTL := time.Duration(request.GetExecutionLeaseTtlSeconds()) * time.Second
	if strings.TrimSpace(controlPlaneNodeID) != "" && leaseTTL <= 0 {
		return nil, errord.ToGRPC(fmt.Errorf("control-plane allocation requires a positive execution lease TTL: %w", errord.ErrInvalidArgument))
	}
	leaseExpiresAt := time.Now().Add(leaseTTL).UTC()
	unlockLifecycle := controller.LockAllocationLifecycle(request.GetAllocationID())
	defer unlockLifecycle()
	if controller.VerifiedEnforcementManifest(request.GetAllocationID()) != nil {
		if controller.AllocationRequestDigest(request.GetAllocationID()) != requestDigest {
			return nil, errord.ToGRPC(fmt.Errorf("allocation request differs from the durable contract: %w", errord.ErrFailedPrecondition))
		}
		resp, active, replayErr := controller.ExistingActiveStartResponseWithLifecycleHeld(ctx, request)
		if replayErr != nil {
			return resp, errord.ToGRPC(replayErr)
		}
		if !active {
			return nil, errord.ToGRPC(fmt.Errorf("durably verified allocation has no active runtime: %w", errord.ErrFailedPrecondition))
		}
		if err := controller.RenewExecutionLeases(map[string]time.Duration{request.GetAllocationID(): leaseTTL}, time.Now().UTC()); err != nil {
			return nil, errord.ToGRPC(err)
		}
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "replayed")
		return resp, nil
	}
	// A live replay is defined by the immutable request digest and
	// verified enforcement manifest above. Current node policy may legitimately differ
	// after a config or runtime identity change; applying it retroactively would
	// break idempotency. New creates still derive and verify the complete current
	// requirement contract before any allocation side effect.
	if err = h.verifyRequestCapabilityRequirements(request); err != nil {
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "requirement_mismatch")
		op.SetErrorStatus("allocation capability requirements do not match request")
		return nil, fmt.Errorf("derive allocation capability requirements: %w", err)
	}
	preCreateObservedAt := time.Now().UTC()
	admitted, verification, err := h.admitCapabilityRequirements(request.GetCapabilityRequirements(), preCreateObservedAt)
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "pre_create_failed")
		op.SetErrorStatus("allocation capability gate failed")
		return nil, fmt.Errorf("verify allocation capabilities before create: %w", err)
	}
	err = controller.StoreAllocationIntent(request.GetAllocationID(), controlPlaneNodeID, requestDigest, leaseExpiresAt, request.GetResources(), admitted, request.GetDeclaredOutputs(), request.GetRootfsSnapshot())
	if err != nil {
		op.SetErrorStatus("persist allocation capability requirements failed")
		return nil, err
	}
	resp, err := controller.StartWithLifecycleHeld(ctx, request)
	if err != nil || resp == nil || resp.GetAllocationID() == "" {
		if err == nil {
			if resp == nil {
				err = fmt.Errorf("allocation start returned no response")
			} else {
				err = fmt.Errorf("allocation start returned no allocation identity")
			}
		}
		op.SetErrorStatus("allocation start failed")
		return resp, errord.ToGRPC(err)
	}
	admitted, verification, err = h.verifyPostCreateCapabilityRequirements(ctx, request.GetAllocationID(), request.GetCapabilityRequirements(), time.Now())
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "post_create_failed")
		err = h.scheduleCapabilityTermination(request.GetAllocationID(), fmt.Errorf("verify allocation capabilities after create: %w", err))
		op.SetErrorStatus("post-create capability enforcement failed")
		return nil, err
	}
	var conditionSet *capabilityv1.CapabilityConditionSet
	conditionSet, err = h.allocationController().ReplaceCapabilityConditions(request.GetAllocationID(), verification, time.Now().UTC())
	if err != nil {
		return nil, h.scheduleCapabilityTermination(request.GetAllocationID(), fmt.Errorf("build post-create capability conditions: %w", err))
	}
	resp.CapabilityVerification = conditionSet
	h.controlPlaneReports.ReportCapabilityConditions(request.GetAllocationID(), conditionSet)
	metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "verified")
	return resp, nil
}

func (h *sandboxService) verifyPreparedAllocationCapabilities(ctx context.Context, request *runtime.StartRequest, handler contract.AllocationRuntime, containerID string) error {
	if request == nil || handler == nil {
		return fmt.Errorf("allocation request and runtime handler are required")
	}
	if request.GetAllocationID() != "" && request.GetAllocationID() != containerID {
		return fmt.Errorf("prepared runtime identity %q differs from allocation identity %q", containerID, request.GetAllocationID())
	}
	manifest, err := handler.AllocationEnforcementManifest(ctx, containerID)
	if err != nil {
		return fmt.Errorf("read immutable runtime enforcement manifest: %w", err)
	}
	if manifest.GetMemoryLimitBytes() > 0 {
		if err := h.containerManager.BindCgroupMemoryDomain(
			manifest.GetCgroupPath(), containerID, manifest.GetMemoryLimitBytes(),
			manifest.GetCgroupBootID(), manifest.GetCgroupMountIdentity(),
			manifest.GetCgroupParentInode(), manifest.GetCgroupLeafInode(),
		); err != nil {
			return fmt.Errorf("persist allocation cgroup memory identity: %w", err)
		}
	}
	durableDependencies := h.allocationController().CapabilityRequirements(containerID)
	dependencies := request.GetCapabilityRequirements()
	if allocation.IsInternalConformance(ctx) {
		keys, deriveErr := capabilitycontract.DeriveRequirements(h.requirementInput(request, false))
		if deriveErr != nil {
			return fmt.Errorf("derive internal conformance requirements: %w", deriveErr)
		}
		dependencies = make([]*capabilityv1.CapabilityRequirement, 0, len(keys))
		for _, key := range keys {
			definition, ok := capabilitycontract.PlatformDefinition(key.GetPlatform())
			if key.GetExtension() != nil || !ok || definition.LossPolicy != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
				continue
			}
			dependencies = append(dependencies, &capabilityv1.CapabilityRequirement{Key: capabilitycontract.CloneKey(key), LossPolicy: definition.LossPolicy})
		}
	} else {
		currentDependencies, _, admitErr := h.admitCapabilityRequirements(dependencies, time.Now().UTC())
		if admitErr != nil {
			return fmt.Errorf("revalidate current pre-activation capability observation: %w", admitErr)
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
		request.CapabilityRequirements = currentDependencies
		dependencies = currentDependencies
	}

	var verifier contract.AllocationCapabilityVerifier
	verifiedKeys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			continue
		}
		if dependency.GetKey().GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT ||
			dependency.GetKey().GetPlatform() == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT {
			if err := h.verifyPreparedEgressPolicy(ctx, request, containerID); err != nil {
				return err
			}
			verifiedKeys = append(verifiedKeys, capabilitycontract.CloneKey(dependency.GetKey()))
			continue
		}
		if verifier == nil {
			var ok bool
			verifier, ok = handler.(contract.AllocationCapabilityVerifier)
			if !ok {
				return fmt.Errorf("runsc has no allocation capability verifier")
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
	return h.allocationController().StoreVerifiedEnforcementManifest(containerID, manifest, verifiedKeys, time.Now().UTC())
}

func (h *sandboxService) requirementInput(request *runtime.StartRequest, erofs bool) capabilitycontract.RequirementInput {
	resources := request.GetResources()
	template := request.GetEnvironment()
	policySpec := request.GetNetwork()
	policyMode := networkpolicy.Mode(policySpec)
	return capabilitycontract.RequirementInput{
		NetworkMode:                     startplan.EffectiveNetworkMode(h.config.NatBackend, request),
		NetworkBackend:                  h.config.PluginConfig.NetworkConfig.NatBackend,
		RequiresDNSPolicyEnforcement:    policyMode == networkpolicy.EnforcementDNSDeny,
		RequiresStrictEgressEnforcement: policyMode == networkpolicy.EnforcementStrict && networkpolicy.StrictNeedsEgressd(policySpec),
		MemoryLimitBytes:                resources.GetLimits().GetMemoryBytes(),
		RootfsWritable:                  !template.GetRootfs().GetReadonly(),
		EphemeralStorageLimitBytes:      resources.GetLimits().GetEphemeralStorageBytes(),
		EROFSBacking:                    erofs,
		RootfsSnapshot:                  request.GetRootfsSnapshot() != nil,
		ExtensionCapabilityRequests:     request.GetExtensionCapabilityRequirements(),
	}
}

func dependencyKeys(dependencies []*capabilityv1.CapabilityRequirement, excludeEROFS bool) ([]*capabilityv1.CapabilityKey, error) {
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
	if request == nil || request.GetEnvironment() == nil || request.GetEnvironment().GetRootfs() == nil {
		return fmt.Errorf("environment template and rootfs are required")
	}
	if strings.TrimSpace(request.GetAllocationID()) == "" {
		return fmt.Errorf("allocation id is required")
	}
	derived, err := capabilitycontract.DeriveRequirements(h.requirementInput(request, false))
	if err != nil {
		return err
	}
	supplied, err := dependencyKeys(request.GetCapabilityRequirements(), true)
	if err != nil {
		return fmt.Errorf("validate supplied dependencies: %w", err)
	}
	if !capabilitycontract.RequirementKeysEqual(derived, supplied) {
		return fmt.Errorf("supplied dependencies do not exactly match request-derived requirements")
	}
	return nil
}

func (h *sandboxService) verifyRootfsCapabilityRequirements(ctx context.Context, request *runtime.StartRequest, rootfs *environmentcache.RootFS) error {
	if allocation.IsInternalConformance(ctx) {
		return nil
	}
	if rootfs == nil || strings.TrimSpace(rootfs.Path()) == "" {
		return fmt.Errorf("materialized rootfs and immutable mount descriptor are required")
	}
	mount := rootfsview.ImmutableMountFromProto(rootfs.ImmutableMount())
	if err := rootfsview.ValidateImmutableMountDescriptor(mount, rootfs.Path()); err != nil {
		return fmt.Errorf("validate rootfs source contract: %w", err)
	}
	derived, err := capabilitycontract.DeriveRequirements(h.requirementInput(request, mount.HasFilesystem("erofs")))
	if err != nil {
		return err
	}
	supplied, err := dependencyKeys(request.GetCapabilityRequirements(), false)
	if err != nil {
		return fmt.Errorf("validate supplied dependencies: %w", err)
	}
	if !capabilitycontract.RequirementKeysEqual(derived, supplied) {
		return fmt.Errorf("supplied dependencies do not exactly match rootfs source-derived requirements (filesystem=%s)", mount.Filesystem)
	}
	// Rootfs materialization may take longer than a health observation's TTL.
	// Rebind the exact requirement set to the manager's current snapshot before
	// bundle, filestore, cgroup, or runtime state is created. The request digest
	// intentionally excludes observation identity, so replacing placement
	// evidence here does not change the allocation's immutable workload contract.
	admitted, _, err := h.admitCapabilityRequirements(request.GetCapabilityRequirements(), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("revalidate capabilities after rootfs materialization: %w", err)
	}
	request.CapabilityRequirements = admitted
	return nil
}

func (h *sandboxService) verifyPostCreateCapabilityRequirements(ctx context.Context, containerID string, dependencies []*capabilityv1.CapabilityRequirement, now time.Time) ([]*capabilityv1.CapabilityRequirement, []*capabilityv1.CapabilityCondition, error) {
	admitted, conditions, err := h.admitCapabilityRequirements(dependencies, now)
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
	verifiedManifest := h.allocationController().VerifiedEnforcementManifest(containerID)
	verifiedByManifest := make(map[string]struct{})
	if verifiedManifest != nil {
		verifiedKeys, keyErr := allocation.RequiredEnforcementKeys(verifiedManifest, h.allocationController().CapabilityRequirements(containerID))
		if keyErr != nil {
			return nil, nil, fmt.Errorf("derive persisted enforcement verification: %w", keyErr)
		}
		for _, key := range verifiedKeys {
			id, keyErr := capabilitycontract.KeyID(key)
			if keyErr != nil {
				return nil, nil, fmt.Errorf("validate persisted enforcement verification: %w", keyErr)
			}
			if _, duplicate := verifiedByManifest[id]; duplicate {
				return nil, nil, fmt.Errorf("persisted enforcement verification contains duplicate capability %q", id)
			}
			verifiedByManifest[id] = struct{}{}
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
		if _, ok := verifiedByManifest[id]; !ok {
			return nil, nil, fmt.Errorf("capability %q has no durable create-before-start verification", id)
		}
		delete(verifiedByManifest, id)
		if condition := byKey[id]; condition != nil {
			condition.Message = "runtime-specific enforcement verified before workload start"
		}
	}
	if len(verifiedByManifest) != 0 {
		return nil, nil, fmt.Errorf("enforcement verification contains capabilities outside the immutable requirement set")
	}
	return admitted, conditions, nil
}

func (h *sandboxService) admitCapabilityRequirements(dependencies []*capabilityv1.CapabilityRequirement, now time.Time) ([]*capabilityv1.CapabilityRequirement, []*capabilityv1.CapabilityCondition, error) {
	if len(dependencies) == 0 {
		return nil, nil, nil
	}
	if h.capabilityManager == nil {
		return nil, nil, fmt.Errorf("capability manager is unavailable")
	}
	return h.capabilityManager.AdmitDependencies(dependencies, now)
}

func (h *sandboxService) Delete(ctx context.Context, request *runtime.DeleteRequest) (response *runtime.DeleteResponse, err error) {
	return h.delete(ctx, request, "")
}

func (h *sandboxService) delete(ctx context.Context, request *runtime.DeleteRequest, controlPlaneNodeID string) (response *runtime.DeleteResponse, err error) {
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
	controller := h.allocationController()
	if controlPlaneNodeID != "" {
		resp, err := controller.DeleteControlPlane(ctx, request, controlPlaneNodeID)
		if err != nil {
			op.SetErrorStatus("allocation delete failed")
			return resp, allocationDeleteGRPCError(err)
		}
		return resp, nil
	}
	resp, err := controller.Delete(ctx, request)
	if err != nil {
		op.SetErrorStatus("allocation delete failed")
		return resp, allocationDeleteGRPCError(err)
	}
	return resp, nil
}

func allocationDeleteGRPCError(err error) error {
	mapped := errord.ToGRPC(err)
	if !allocation.IsRootfsSnapshotSealingError(err) {
		return mapped
	}
	switch grpcstatus.Code(mapped) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.ResourceExhausted, codes.Unimplemented, codes.Canceled, codes.DeadlineExceeded:
		return mapped
	default:
		// Aborted identifies a retryable failure inside the rootfs sealing
		// barrier. Ordinary cleanup failures keep their own status and cannot
		// exhaust the snapshot publication budget.
		return grpcstatus.Error(codes.Aborted, err.Error())
	}
}
