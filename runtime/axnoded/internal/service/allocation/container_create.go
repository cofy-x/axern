package allocation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

var errRuntimeCleanupPending = errors.New("runtime cleanup remains pending")

// createAllocation deliberately uses the OCI create/start split. The
// created runtime and all host-side storage/cgroup state exist at the gate,
// while the workload process has not started and therefore cannot race a
// short-lived command against create-time enforcement verification.
func (h *Controller) createAllocation(
	ctx context.Context,
	lrt *environmentcache.PreparedEnvironment,
	startRequest *apipb.StartRequest,
	templateRequest *apipb.CreateContainerRequest,
	request *apipb.CreateContainerRequest,
	handler contract.SandboxRuntime,
	resource container.OccupiedResource,
	phaseRecorder contract.StartupPhaseRecorder,
) (*apipb.CreateContainerResponse, string, error) {
	traceID, spanID := trace.GetContextID(ctx)
	response := new(apipb.CreateContainerResponse)
	if handler == nil || resource.ID == "" || resource.ID != request.GetID() {
		return response, "", errors.Join(errors.New("allocation resources are missing or inconsistent"), errRuntimeCleanupPending)
	}
	allocationRuntime, ok := handler.(contract.AllocationRuntime)
	if !ok {
		err := fmt.Errorf("runsc does not implement the allocation create/start contract")
		return response, "", errors.Join(err, errRuntimeCleanupPending)
	}

	options, err := h.createHandlerOptions(traceID.String(), spanID.String(), lrt, templateRequest, resource, phaseRecorder)
	if err != nil {
		return response, "", errors.Join(err, errRuntimeCleanupPending)
	}
	prepared, err := allocationRuntime.PrepareContainer(ctx, request, options)
	if err != nil {
		h.cleanupFailedContainerCreate(traceID.String(), resource.ID, preparedContainerMetadata(prepared))
		return response, "", errors.Join(err, errRuntimeCleanupPending)
	}
	cleanupPrepared := func(cause error) error {
		// The service facade owns the single ordered rollback after any resource
		// allocation. Returning this marker keeps allocation state and image
		// leases durable until runtime deletion has crossed the exit-state barrier.
		return errors.Join(cause, errRuntimeCleanupPending)
	}
	if prepared == nil || prepared.Metadata == nil || prepared.ContainerID != resource.ID {
		return response, "", cleanupPrepared(errors.New("runtime returned an invalid prepared container"))
	}
	// Persist ownership before running the gate. A crash or failed cleanup in
	// the create-before-start window must remain discoverable by normal runtime
	// inventory and the ordered Delete path.
	if err := h.containers().StoreMetadata(resource.ID, prepared.Metadata); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("persist prepared container metadata: %w", err))
	}
	if h.preActivationCapabilityGate == nil {
		return response, "", cleanupPrepared(errors.New("allocation pre-activation capability gate is unavailable"))
	}
	if err := h.preActivationCapabilityGate(ctx, startRequest, allocationRuntime, resource.ID); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("verify allocation before activation: %w", err))
	}

	metaData, err := allocationRuntime.StartPreparedContainer(ctx, prepared, options)
	if err != nil {
		return response, "", cleanupPrepared(err)
	}
	if metaData == nil {
		return response, "", cleanupPrepared(errors.New("runtime returned no activated container metadata"))
	}
	response.ID = resource.ID
	if err := h.containers().StoreMetadata(resource.ID, metaData); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("persist activated container metadata: %w", err))
	}
	if err := h.registerCreatedContainerLifecycle(ctx, resource.ID, metaData, allocationRuntime); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("register activated container monitor: %w", err))
	}
	return response, containerIPFromResource(resource), nil
}

// registerCreatedContainerLifecycle establishes the runtime Wait observer before
// consulting the runtime's lossy list view. A short-lived process may already
// be absent or reported only as stopped by ListContainers, while Wait can still
// recover its durable exit record. A running List entry may enrich runtime
// identity; an exited entry is ignored because its PID may already be reused.
// Wait remains the sole create-time source of terminal lifecycle evidence.
func (h *Controller) registerCreatedContainerLifecycle(
	ctx context.Context,
	containerID string,
	metaData *apipb.ContainerMetadata,
	handler contract.SandboxRuntime,
) error {
	if err := h.containers().StartMonitor(containerID, metaData); err != nil {
		return err
	}
	h.syncCreatedContainerStatus(ctx, containerID, handler)
	return nil
}

func (h *Controller) cleanupCreatedRuntime(handler contract.SandboxRuntime, resource container.OccupiedResource, cause error) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, deleteErr := handler.DeleteContainer(cleanupCtx, &apipb.DeleteContainerRequest{ID: resource.ID, Timeout: 0}, contract.HandlerOptions{ContainerID: resource.ID, ForceDelete: true})
	if deleteErr != nil && !isDeleteNotFound(deleteErr) {
		// Runtime ownership is still uncertain. Releasing its cgroup, network, or
		// storage would make the surviving process unaccounted and uncleanable.
		return errors.Join(cause, errRuntimeCleanupPending, fmt.Errorf("delete unregistered runtime: %w", deleteErr))
	}
	if err := h.sandboxNetworking().CleanupActivationNetwork(resource); err != nil {
		return errors.Join(cause, errRuntimeCleanupPending, fmt.Errorf("cleanup unregistered runtime network: %w", err))
	}
	if err := h.containers().DeleteAfterConfirmedRuntimeDelete(resource.ID, resource); err != nil {
		return errors.Join(cause, errRuntimeCleanupPending, fmt.Errorf("finalize failed runtime resources: %w", err))
	}
	return cause
}

func preparedContainerMetadata(prepared *contract.PreparedContainer) *apipb.ContainerMetadata {
	if prepared == nil {
		return nil
	}
	return prepared.Metadata
}

func (h *Controller) syncCreatedContainerStatus(ctx context.Context, containerID string, handler contract.SandboxRuntime) {
	states, err := handler.ListContainers(ctx, contract.HandlerOptions{})
	if err != nil {
		logrus.Warnf("sync created container %s status skipped: list runtime state failed: %v", containerID, err)
		return
	}
	for _, state := range states {
		if state == nil || state.ID != containerID {
			continue
		}
		if err := h.containers().SyncRuntimeIdentityFromState(containerID, state); err != nil {
			logrus.Warnf("sync created container %s status failed: %v", containerID, err)
		}
		return
	}
}

func (h *Controller) prepareContainerCreate(ctx context.Context, traceID string, request *apipb.CreateContainerRequest, resourceSpec *commonv1.ResourceSpec) (contract.SandboxRuntime, container.OccupiedResource, error) {
	return h.prepareContainerResources(ctx, traceID, request.GetID(), request.GetEnvs(), resourceSpec)
}

// prepareContainerResources is the node-local admission boundary. Allocation
// starts call it before secrets, image mounts, rootfs preparation, or
// runtime artifacts so a rejected memory commitment has no external side
// effects to roll back.
func (h *Controller) prepareContainerResources(ctx context.Context, traceID, containerID string, envs []*apipb.KeyValue, resourceSpec *commonv1.ResourceSpec) (contract.SandboxRuntime, container.OccupiedResource, error) {
	var empty container.OccupiedResource
	if h == nil || h.runscHandler == nil {
		return nil, empty, fmt.Errorf("runsc handler unavailable")
	}
	handler := h.runscHandler

	resourceNames := handler.HostRequirements().Resources
	if resourceNames == nil {
		resourceNames = []resourcemanager.ResourceName{}
	}

	ownerKind := cgroupLeaseOwnerKind(ctx)
	memoryRequest := resourceSpec.GetRequests().GetMemoryBytes()
	resource, err := h.containers().Occupy(resourcemanager.AllocateOption{
		Context:             ctx,
		ContainerID:         containerID,
		EnvID:               envValue(envs, config.SandboxEnvKey),
		TraceID:             traceID,
		MemoryRequestBytes:  memoryRequest,
		MemoryLimitBytes:    resourceSpec.GetLimits().GetMemoryBytes(),
		CapacityChargeBytes: cgroupCapacityCharge(ctx, memoryRequest),
		CgroupOwnerKind:     ownerKind,
	}, resourceNames...)
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("occpuy resource failed: %v", err)
		return nil, empty, err
	}

	return handler, resource, nil
}

func cgroupLeaseOwnerKind(ctx context.Context) apipb.CgroupLeaseOwnerKind {
	if IsInternalConformance(ctx) {
		return apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE
	}
	return apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD
}

func cgroupCapacityCharge(ctx context.Context, requested int64) int64 {
	if IsInternalConformance(ctx) {
		// Every destructive self-test is charged the aggregate certification
		// ceiling even when the behavior under test has no OCI memory limit.
		// This keeps storage evidence independent from memory-limit evidence
		// while admission still reserves the complete node-owned domain.
		return config.RuntimeConformanceMemoryMaxBytes
	}
	return requested
}

func envValue(envs []*apipb.KeyValue, key string) string {
	for _, env := range envs {
		if env.GetKey() == key {
			return env.GetValue()
		}
	}
	return ""
}

func (h *Controller) createHandlerOptions(
	traceID, spanID string,
	lrt *environmentcache.PreparedEnvironment,
	templateRequest *apipb.CreateContainerRequest,
	resource container.OccupiedResource,
	phaseRecorder contract.StartupPhaseRecorder,
) (contract.HandlerOptions, error) {
	var templateSource *runtimeoci.TemplateOptions
	if templateRequest != nil {
		templateSource = &runtimeoci.TemplateOptions{Request: templateRequest}
	}

	var networkNamespacePath, sandboxIP string
	if networkResource, ok := resource.Resources[resourcemanager.InterfaceResourceName]; ok {
		netDevice := &resourcemanager.NetResource{}
		if err := netDevice.FromString(networkResource); err != nil {
			return contract.HandlerOptions{}, fmt.Errorf("decode allocation %s network binding: %w", resource.ID, err)
		}
		if netDevice.Ip == nil || netDevice.NetNSPath == "" {
			return contract.HandlerOptions{}, fmt.Errorf("allocation %s has an incomplete network binding", resource.ID)
		}
		networkNamespacePath = netDevice.NetNSPath
		sandboxIP = netDevice.Ip.String()
	}

	return contract.HandlerOptions{
		TraceID:               traceID,
		SpanID:                spanID,
		ContainerID:           resource.ID,
		StartupPhaseRecorder:  phaseRecorder,
		AllocatedResources:    resource.Resources,
		CgroupPath:            resource.Resources[resourcemanager.CgroupResourceName],
		RootfsType:            rootfsTypeFromPreparedEnvironment(lrt),
		BundleTemplateCarrier: lrt,
		BundleTemplateSource:  templateSource,
		NetworkNamespacePath:  networkNamespacePath,
		SandboxIP:             sandboxIP,
		ExecutionProfile:      executionProfileFromPreparedEnvironment(lrt),
	}, nil
}

func (h *Controller) cleanupFailedContainerCreate(traceID, containerID string, metaData *apipb.ContainerMetadata) {
	if metaData != nil {
		h.logStdFileSnippet(traceID, containerID, "stderr", metaData.Stderr)
		h.logStdFileSnippet(traceID, containerID, "stdout", metaData.Stdout)
	}
	h.logSandboxdDiagnostics(traceID, containerID, metaData)
	if metaData == nil {
		return
	}
	h.cleanupStdFile(traceID, metaData.Stderr)
	h.cleanupStdFile(traceID, metaData.Stdout)
}

func containerIPFromResource(resource container.OccupiedResource) string {
	device, ok := resource.Resources[resourcemanager.InterfaceResourceName]
	if !ok {
		return ""
	}

	netDevice := &resourcemanager.NetResource{}
	if err := netDevice.FromString(device); err != nil {
		return ""
	}
	return netDevice.Ip.String()
}

func rootfsTypeFromPreparedEnvironment(lrt *environmentcache.PreparedEnvironment) string {
	if lrt == nil || lrt.RootFS == nil {
		return contract.StartupRootfsTypeUnknown
	}
	switch lrt.RootFS.RootfsTypeLabel() {
	case contract.StartupRootfsTypeLocal, contract.StartupRootfsTypeImage:
		return lrt.RootFS.RootfsTypeLabel()
	default:
		return contract.StartupRootfsTypeUnknown
	}
}
