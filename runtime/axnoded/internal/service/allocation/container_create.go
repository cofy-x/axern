package allocation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var errRuntimeCleanupPending = errors.New("runtime cleanup remains pending")

func (h *Controller) createContainer(
	ctx context.Context,
	lrt *langrtmanager.LanguageRuntime,
	templateRequest *apipb.CreateContainerRequest,
	request *apipb.CreateContainerRequest,
	resourceSpec *commonv1.ResourceSpec,
	phaseRecorder contract.StartupPhaseRecorder,
) (*apipb.CreateContainerResponse, string, error) {
	traceID, spanID := trace.GetContextID(ctx)
	response := new(apipb.CreateContainerResponse)
	start := time.Now()
	var err error
	defer func() {
		if err != nil {
			logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("CreateContainer failed, traceID: %v, spanID: %v, err: %v", traceID, spanID, err)
		}
	}()

	resourceAllocateStart := time.Now()
	resourceCtx, resourceSpan := sdkobs.Start(ctx, sandboxobs.SpanResourceAllocate,
		attribute.String(sdkobs.AttrAllocationID, request.GetID()),
		attribute.String(sdkobs.AttrRuntime, request.GetRuntime()),
	)
	handler, resource, err := h.prepareContainerCreate(ctx, traceID.String(), request, resourceSpec)
	if err != nil {
		resourceSpan.RecordError(err)
		resourceSpan.SetStatus(codes.Error, "resource allocate")
	} else {
		resourceSpan.SetAttributes(attribute.String(sdkobs.AttrResult, sdkobs.ResultOK))
	}
	resourceSpan.End()
	if phaseRecorder != nil {
		phaseRecorder.RecordStartupPhase(contract.StartupPhaseResourceAllocate, time.Since(resourceAllocateStart))
	}
	if err != nil {
		return response, "", err
	}

	_, runtimeSpan := sdkobs.Start(resourceCtx, sandboxobs.SpanRuntimeCreate,
		attribute.String(sdkobs.AttrAllocationID, resource.ID),
		attribute.String(sdkobs.AttrRuntime, request.GetRuntime()),
	)
	metaData, err := handler.CreateContainer(ctx, request, h.createHandlerOptions(traceID.String(), spanID.String(), lrt, templateRequest, resource, phaseRecorder))
	if err != nil {
		runtimeSpan.RecordError(err)
		runtimeSpan.SetStatus(codes.Error, "runtime create")
	} else {
		runtimeSpan.SetAttributes(attribute.String(sdkobs.AttrResult, sdkobs.ResultOK))
	}
	runtimeSpan.End()
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("runtime handler create container failed: %v", err)
		h.cleanupFailedContainerCreate(traceID.String(), resource.ID, metaData)
		return response, "", h.cleanupCreatedRuntime(handler, resource, err)
	}

	response.ID = resource.ID
	if err := h.containers().StoreMetadata(resource.ID, metaData); err != nil {
		return response, "", h.cleanupCreatedRuntime(handler, resource, fmt.Errorf("persist created container metadata: %w", err))
	}
	if err := h.containers().SetResources(resource.ID, request.Resource, resourceSpec); err != nil {
		return response, "", h.cleanupCreatedRuntime(handler, resource, fmt.Errorf("persist created container resources: %w", err))
	}
	if err := h.registerCreatedContainerLifecycle(ctx, resource.ID, metaData, handler); err != nil {
		return response, "", h.cleanupCreatedRuntime(handler, resource, fmt.Errorf("register created container monitor: %w", err))
	}
	logrus.WithField(trace.ContextKeyTraceId, traceID).Infof("CreateContainer %s success, traceID: %v, spanID: %v, cost: %v", resource.ID, traceID, spanID, time.Since(start).String())

	return response, containerIPFromResource(resource), nil
}

// createManagedContainer deliberately uses the OCI create/start split. The
// created runtime and all host-side storage/cgroup state exist at the gate,
// while the workload process has not started and therefore cannot race a
// short-lived command against create-time enforcement verification.
func (h *Controller) createManagedContainer(
	ctx context.Context,
	lrt *langrtmanager.LanguageRuntime,
	startRequest *apipb.StartRequest,
	templateRequest *apipb.CreateContainerRequest,
	request *apipb.CreateContainerRequest,
	handler contract.RuntimeHandler,
	resource container.OccupiedResource,
	phaseRecorder contract.StartupPhaseRecorder,
) (*apipb.CreateContainerResponse, string, error) {
	traceID, spanID := trace.GetContextID(ctx)
	response := new(apipb.CreateContainerResponse)
	if handler == nil || resource.ID == "" || resource.ID != request.GetID() {
		return response, "", errors.Join(errors.New("managed allocation resources are missing or inconsistent"), errRuntimeCleanupPending)
	}
	managed, ok := handler.(contract.ManagedRuntimeHandler)
	if !ok {
		err := fmt.Errorf("runtime %q does not implement the managed create/start contract", handler.Name())
		return response, "", errors.Join(err, errRuntimeCleanupPending)
	}

	options := h.createHandlerOptions(traceID.String(), spanID.String(), lrt, templateRequest, resource, phaseRecorder)
	prepared, err := managed.PrepareContainer(ctx, request, options)
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
	if prepared == nil || prepared.Metadata == nil || prepared.Metadata.GetID() != resource.ID || prepared.ContainerID != resource.ID {
		return response, "", cleanupPrepared(errors.New("managed runtime returned an invalid prepared container"))
	}
	// Persist ownership before running the gate. A crash or failed cleanup in
	// the create-before-start window must remain discoverable by normal runtime
	// inventory and the ordered Delete path.
	if err := h.containers().StoreMetadata(resource.ID, prepared.Metadata); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("persist prepared container metadata: %w", err))
	}
	if h.preActivationCapabilityGate == nil {
		return response, "", cleanupPrepared(errors.New("managed allocation pre-activation capability gate is unavailable"))
	}
	if err := h.preActivationCapabilityGate(ctx, startRequest, managed, resource.ID); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("verify managed allocation before activation: %w", err))
	}

	metaData, err := managed.StartPreparedContainer(ctx, prepared, options)
	if err != nil {
		return response, "", cleanupPrepared(err)
	}
	response.ID = resource.ID
	if err := h.containers().StoreMetadata(resource.ID, metaData); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("persist activated container metadata: %w", err))
	}
	if err := h.containers().SetResources(resource.ID, request.Resource, startRequest.GetResources()); err != nil {
		return response, "", cleanupPrepared(fmt.Errorf("persist activated container resources: %w", err))
	}
	if err := h.registerCreatedContainerLifecycle(ctx, resource.ID, metaData, managed); err != nil {
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
	handler contract.RuntimeHandler,
) error {
	if err := h.containers().StartMonitor(metaData); err != nil {
		return err
	}
	h.syncCreatedContainerStatus(ctx, containerID, handler)
	return nil
}

func (h *Controller) cleanupCreatedRuntime(handler contract.RuntimeHandler, resource container.OccupiedResource, cause error) error {
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

func (h *Controller) syncCreatedContainerStatus(ctx context.Context, containerID string, handler contract.RuntimeHandler) {
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

func (h *Controller) prepareContainerCreate(ctx context.Context, traceID string, request *apipb.CreateContainerRequest, resourceSpec *commonv1.ResourceSpec) (contract.RuntimeHandler, container.OccupiedResource, error) {
	attempt, _ := strconv.ParseInt(strings.TrimSpace(request.GetLabels()[workloadidentity.LabelKeyAllocationAttempt]), 10, 64)
	return h.prepareContainerResources(ctx, traceID, request.GetRuntime(), request.GetID(), attempt, request.GetEnvs(), resourceSpec)
}

// prepareContainerResources is the node-local admission boundary. Managed
// starts call it before secrets, volumes, image mounts, rootfs preparation, or
// runtime artifacts so a rejected memory commitment has no external side
// effects to roll back.
func (h *Controller) prepareContainerResources(ctx context.Context, traceID, runtimeName, containerID string, allocationAttempt int64, envs []*apipb.KeyValue, resourceSpec *commonv1.ResourceSpec) (contract.RuntimeHandler, container.OccupiedResource, error) {
	var empty container.OccupiedResource

	if err := h.checkRuntime(runtimeName); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Debugf("check runtime failed: %v", err)
		return nil, empty, errord.ErrNotImplemented
	}

	handler, err := h.runtimeHandler(runtimeName)
	if err != nil {
		return nil, empty, errord.ErrNotImplemented
	}

	resourceNames := handler.Requirements().Resources
	if resourceNames == nil {
		resourceNames = []resourcemanager.ResourceName{}
	}

	ownerKind := cgroupLeaseOwnerKind(ctx)
	memoryReservation := cgroupMemoryReservation(ctx, resourceSpec.GetRequests().GetMemoryBytes())
	resource, err := h.containers().Occupy(resourcemanager.AllocateOption{
		Context:            ctx,
		ContainerID:        containerID,
		EnvID:              envValue(envs, config.SandboxEnvKey),
		TraceID:            traceID,
		FunctionName:       envValue(envs, config.SandboxFunctionNameKey),
		MemoryRequestBytes: memoryReservation,
		MemoryLimitBytes:   resourceSpec.GetLimits().GetMemoryBytes(),
		AllocationAttempt:  allocationAttempt,
		RuntimeName:        runtimeName,
		CgroupOwnerKind:    ownerKind,
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

func cgroupMemoryReservation(ctx context.Context, requested int64) int64 {
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
	lrt *langrtmanager.LanguageRuntime,
	templateRequest *apipb.CreateContainerRequest,
	resource container.OccupiedResource,
	phaseRecorder contract.StartupPhaseRecorder,
) contract.HandlerOptions {
	var templateSource *runtimeoci.TemplateOptions
	if templateRequest != nil {
		templateSource = &runtimeoci.TemplateOptions{Request: templateRequest}
	}

	return contract.HandlerOptions{
		TraceID:               traceID,
		SpanID:                spanID,
		ContainerID:           resource.ID,
		StartupPhaseRecorder:  phaseRecorder,
		AllocatedResources:    resource.Resources,
		CgroupPath:            resource.Resources[resourcemanager.CgroupResourceName],
		RootfsType:            rootfsTypeFromLanguageRuntime(lrt),
		BundleTemplateCarrier: lrt,
		BundleTemplateSource:  templateSource,
		AdditionalAnnotations: resource.ToLabels(),
		ExecutionProfile:      executionProfileFromLanguageRuntime(lrt),
	}
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

func rootfsTypeFromLanguageRuntime(lrt *langrtmanager.LanguageRuntime) string {
	if lrt == nil || lrt.RootFS == nil {
		return contract.StartupRootfsTypeUnknown
	}
	switch lrt.RootFS.RootfsTypeLabel() {
	case contract.StartupRootfsTypeLocal, contract.StartupRootfsTypeImage, contract.StartupRootfsTypeS3:
		return lrt.RootFS.RootfsTypeLabel()
	default:
		return contract.StartupRootfsTypeUnknown
	}
}
