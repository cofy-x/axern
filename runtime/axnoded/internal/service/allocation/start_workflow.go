package allocation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func (h *Controller) ensureLangRuntime(ctx context.Context, fr *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, LangRuntimePrepareSummary, error) {
	rootfsCfg, err := langrtmanager.RootfsConfigFromRuntimeTemplate(fr)
	if err != nil {
		return nil, LangRuntimePrepareSummary{RootfsType: RootfsTypeFromRuntimeTemplate(fr)}, err
	}
	return h.prepareLangRuntime(ctx, fr, rootfsCfg)
}

func (h *Controller) ensureLangRuntimeFromRequest(ctx context.Context, request *runtime.StartRequest) (*langrtmanager.LanguageRuntime, LangRuntimePrepareSummary, error) {
	_, span := sdkobs.Start(ctx, sandboxobs.SpanRootFSPrepare,
		attribute.String(sdkobs.AttrAllocationID, request.GetContainerID()),
		attribute.String(sdkobs.AttrRuntime, request.GetRuntimeTemplate().GetSandbox()),
		attribute.String(sdkobs.AttrRootFSType, RootfsTypeFromRuntimeTemplate(request.GetRuntimeTemplate())),
	)
	defer span.End()
	rootfsCfg, err := startplan.RootfsConfigFromStartRequest(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rootfs config")
		return nil, LangRuntimePrepareSummary{RootfsType: RootfsTypeFromRuntimeTemplate(request.GetRuntimeTemplate())}, err
	}
	fr := request.GetRuntimeTemplate()
	lrt, summary, err := h.prepareLangRuntime(ctx, fr, rootfsCfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "prepare language runtime")
		span.SetAttributes(attribute.String(sdkobs.AttrResult, "error"))
	} else {
		span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"), attribute.Bool("axern.runtime_reused", summary.RuntimeReused))
	}
	return lrt, summary, err
}

func (h *Controller) prepareLangRuntime(ctx context.Context, fr *runtime.RuntimeTemplate, rootfsCfg langrtmanager.RootfsConfig) (*langrtmanager.LanguageRuntime, LangRuntimePrepareSummary, error) {
	summary := LangRuntimePrepareSummary{
		RootfsType: RootfsTypeFromRuntimeTemplate(fr),
	}
	lookupStart := time.Now()
	lrt := h.lrtManager.FindReusableLangRuntime(fr, rootfsCfg)
	summary.LangRuntimeLookupTime = time.Since(lookupStart)
	if lrt != nil {
		summary.RuntimeReused = true
		return lrt, summary, nil
	}

	resolveStart := time.Now()
	resolvedRootfsCfg, err := h.lrtManager.ResolveRootfsConfig(rootfsCfg)
	if err != nil {
		return nil, summary, err
	}
	summary.Steps = append(summary.Steps, StartupStepSample{
		Phase:    contract.StartupPhaseRootfsPrepare,
		Step:     contract.StartupStepRootfsResolve,
		Duration: startupStepDurationSince(resolveStart),
	})
	prepareStart := time.Now()
	result, err := h.lrtManager.AddLangRuntime(ctx, fr, resolvedRootfsCfg, true)
	summary.RootfsPrepareTime = time.Since(prepareStart)
	summary.Steps = append(summary.Steps, startupStepSamplesFromRootfsReport(result.RootfsReport)...)
	summary.RuntimeReused = !result.Created
	return result.Runtime, summary, err
}

func startupStepSamplesFromRootfsReport(report langrtmanager.RootfsPrepareReport) []StartupStepSample {
	out := make([]StartupStepSample, 0, len(report.Steps))
	for _, sample := range report.Steps {
		out = append(out, StartupStepSample{
			Phase:    sample.Phase,
			Step:     sample.Step,
			Duration: sample.Duration,
		})
	}
	return out
}

func startupStepDurationSince(started time.Time) time.Duration {
	duration := time.Since(started)
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}

func (h *Controller) cleanupFailedStart(ctx context.Context, containerID string) error {
	return h.cleanupFailedStartWithResource(ctx, containerID, container.OccupiedResource{})
}

func (h *Controller) cleanupFailedStartWithResource(ctx context.Context, containerID string, reserved container.OccupiedResource) error {
	h.sandboxNetworking().CleanupDnatRules(containerID)
	h.sandboxNetworking().CloseHTTPProxyTransports(containerID)
	var resource container.OccupiedResource
	resourceKnown := false
	if _, err := h.containers().Get(containerID); err == nil {
		var deleteErr error
		_, resource, deleteErr = h.deleteContainerRuntime(ctx, &apipb.DeleteContainerRequest{ID: containerID, Timeout: 0})
		if deleteErr != nil && !isDeleteNotFound(deleteErr) {
			return fmt.Errorf("delete failed-start runtime: %w", deleteErr)
		}
		resourceKnown = true
	} else if runtime, ok := h.runtimeMapping(containerID); ok {
		// OCI create may succeed before container metadata is durably indexed.
		// Recover ownership from the allocation record and bundle so a metadata
		// persistence failure cannot strand runtime, network, or resource state.
		handler, handlerErr := h.runtimeHandler(runtime.Sandbox)
		if handlerErr != nil {
			return fmt.Errorf("resolve partial failed-start runtime: %w", handlerErr)
		} else {
			var resourceErr error
			resource, resourceErr = h.containers().CollectResourceByID(containerID)
			if resourceErr != nil && reserved.ID == containerID && len(reserved.Resources) > 0 {
				resource = reserved
				resourceErr = nil
			}
			if resourceErr != nil && !errors.Is(resourceErr, os.ErrNotExist) {
				return fmt.Errorf("collect partial failed-start resources: %w", resourceErr)
			}
			if _, deleteErr := handler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{ID: containerID, Timeout: 0}, contract.HandlerOptions{ContainerID: containerID, ForceDelete: true}); deleteErr != nil && !isDeleteNotFound(deleteErr) {
				return fmt.Errorf("delete partial failed-start runtime: %w", deleteErr)
			} else if resourceErr == nil {
				if networkErr := h.sandboxNetworking().CleanupActivationNetwork(resource); networkErr != nil {
					return fmt.Errorf("cleanup partial failed-start network: %w", networkErr)
				}
				resourceKnown = true
			}
		}
	}
	if _, err := h.nodeVolumes().Unpublish(ctx, containerID); err != nil {
		return fmt.Errorf("unpublish failed-start volumes: %w", err)
	}
	if err := h.releaseAllocationState(containerID); err != nil {
		return fmt.Errorf("release failed-start allocation state: %w", err)
	}
	if resourceKnown {
		if err := h.finalizeFailedContainerDelete(containerID, resource); err != nil {
			return fmt.Errorf("finalize failed-start resources: %w", err)
		}
	}
	return nil
}

func startErrorResponse(message string) *runtime.StartResponse {
	return &runtime.StartResponse{Code: -1, Message: message, ID: ""}
}

func startSuccessResponse(containerID string) *runtime.StartResponse {
	return &runtime.StartResponse{Code: 0, Message: "Succeed", ID: containerID}
}

func (h *Controller) existingActiveStartResponse(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, bool, error) {
	containerID := request.GetContainerID()
	containerID = strings.TrimSpace(containerID)
	if containerID == "" || h == nil || h.containers() == nil {
		return nil, false, nil
	}
	ct, err := h.containers().Get(containerID)
	if err != nil {
		return nil, false, nil
	}
	if ct.Status != nil && ct.Status.Get().State() == runtime.ContainerState_CONTAINER_EXITED {
		return startErrorResponse(fmt.Sprintf("allocation %s already exists in terminal state", containerID)), true, errord.ErrAlreadyExists
	}
	resp := startSuccessResponse(containerID)
	if len(request.GetNodeVolumes()) > 0 {
		published, err := h.nodeVolumes().PublishedForAllocation(ctx, containerID)
		if err != nil {
			return startErrorResponse(fmt.Sprintf("Failed to list published node volumes: %v", err)), true, err
		}
		resp.PublishedVolumes = published
	}
	return resp, true, nil
}

func (h *Controller) startManagedContainer(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	if err := startplan.ValidateStartRequest(request); err != nil {
		return startErrorResponse(err.Error()), err
	}
	generatedAllocationID := false
	if strings.TrimSpace(request.GetContainerID()) == "" {
		allocationID, err := h.containers().ReserveContainerID()
		if err != nil {
			return startErrorResponse(fmt.Sprintf("Failed to reserve allocation id: %v", err)), err
		}
		request.ContainerID = allocationID
		generatedAllocationID = true
	}
	unlockLifecycle := h.allocationLifecycleLocks.Lock(request.GetContainerID())
	defer unlockLifecycle()
	return h.startManagedContainerWithLifecycleHeld(ctx, request, generatedAllocationID)
}

func (h *Controller) startManagedContainerWithLifecycleHeld(ctx context.Context, request *runtime.StartRequest, generatedAllocationID bool) (response *runtime.StartResponse, returnErr error) {
	if resp, ok, err := h.existingActiveStartResponse(ctx, request); ok {
		return resp, err
	}

	recorder := NewStartMetricsRecorder(h.startMetricSink, request.RuntimeTemplate.Sandbox, RootfsTypeFromRuntimeTemplate(request.RuntimeTemplate))
	result := contract.StartupResultError
	succeeded := false
	stateCommitted := false
	resourceReserved := false
	var reservedResource container.OccupiedResource
	defer func() {
		recorder.Finish(result)
		if succeeded {
			return
		}
		if stateCommitted {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := h.cleanupFailedStartWithResource(cleanupCtx, request.GetContainerID(), reservedResource); err != nil {
				returnErr = errors.Join(returnErr, errRuntimeCleanupPending, fmt.Errorf("ordered failed-start cleanup: %w", err))
			}
			return
		}
		if resourceReserved {
			if err := h.containers().Release(reservedResource); err != nil {
				returnErr = errors.Join(returnErr, errRuntimeCleanupPending, fmt.Errorf("retire failed-start resources: %w", err))
			}
		} else if generatedAllocationID {
			h.containers().ReleaseContainerID(request.GetContainerID())
		}
	}()

	extraConfig, _ := startplan.ParseExtraConfig(request.ExtraConfig)
	traceID, _ := trace.GetContextID(ctx)
	resourceAllocateStart := time.Now()
	handler, resource, err := h.prepareContainerResources(
		ctx,
		traceID.String(),
		request.GetRuntimeTemplate().GetSandbox(),
		request.GetContainerID(),
		request.GetAllocationAttempt(),
		nil,
		request.GetResources(),
	)
	if recorder != nil {
		recorder.RecordStartupPhase(contract.StartupPhaseResourceAllocate, time.Since(resourceAllocateStart))
	}
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed node-local resource admission: %v", err)), err
	}
	reservedResource = resource
	resourceReserved = true
	startplan.ApplyResolvedSecretEnv(request, extraConfig)
	secretCleanup, err := startplan.MaterializeResolvedSecretFiles(request, extraConfig)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to materialize secrets: %v", err)), err
	}
	defer func() {
		if !succeeded {
			secretCleanup()
		}
	}()
	publishResult, err := h.nodeVolumes().PublishForStart(ctx, request)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to publish node volumes: %v", err)), err
	}
	request.Mounts = append(request.Mounts, publishResult.RuntimeMounts...)
	defer func() {
		if !succeeded && !stateCommitted {
			_, _ = h.nodeVolumes().Unpublish(ctx, request.GetContainerID())
		}
	}()
	imageMounts, imageMountCleanup, err := h.resolveImageMounts(request, extraConfig)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to resolve image mounts: %v", err)), err
	}
	request.Mounts = append(request.Mounts, imageMounts...)
	defer func() {
		if !succeeded && !stateCommitted {
			imageMountCleanup()
		}
	}()
	workspaceMount, workspaceCleanup, err := h.resolveWorkspaceImage(request, extraConfig)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to resolve workspace image: %v", err)), err
	}
	if workspaceMount != nil {
		request.Mounts = append(request.Mounts, workspaceMount)
	}
	defer func() {
		if !succeeded && !stateCommitted {
			workspaceCleanup()
		}
	}()

	lrt, prepareSummary, err := h.ensureLangRuntimeFromRequest(ctx, request)
	recorder.SetStartClass(prepareSummary.StartClass())
	recorder.SetRootfsType(prepareSummary.RootfsType)
	recorder.RecordStartupPhase(contract.StartupPhaseLangRuntimeLookup, prepareSummary.LangRuntimeLookupTime)
	recorder.RecordStartupPhase(contract.StartupPhaseRootfsPrepare, prepareSummary.RootfsPrepareTime)
	for _, sample := range prepareSummary.Steps {
		recorder.RecordStartupStep(sample.Phase, sample.Step, sample.Duration)
	}
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to add new runtime: %v", request.RuntimeTemplate)), err
	}
	if h.rootfsCapabilityGate != nil {
		if err := h.rootfsCapabilityGate(ctx, request, lrt.RootFS); err != nil {
			return startErrorResponse(fmt.Sprintf("Failed rootfs capability gate: %v", err)), err
		}
	}

	lrt.IncRef()
	defer func() {
		if !succeeded && !stateCommitted {
			lrt.DecRef()
		}
	}()
	if err := h.rememberContainerRuntime(request.GetContainerID(), lrt); err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to persist allocation state: %v", err)), err
	}
	stateCommitted = true

	createRequest := startplan.BuildCreateContainerRequest(
		lrt,
		request,
		startplan.BuildStartLabels(request),
		startplan.BuildStartEnv(lrt, request),
		startplan.EffectiveNetworkMode(h.config.NatBackend, request),
	)
	templateRequest := startplan.BuildBundleTemplateRequest(lrt, request)

	createResponse, containerIP, err := h.createManagedContainer(ctx, lrt, request, templateRequest, createRequest, handler, reservedResource, recorder)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to start: %v", err)), err
	}

	networkStart := time.Now()
	if err := h.configureStartPorts(ctx, createResponse.ID, containerIP, request.Ports); err != nil {
		if len(request.Ports) > 0 {
			recorder.RecordStartupPhase(contract.StartupPhaseNetworkActivate, time.Since(networkStart))
		}
		return startErrorResponse(err.Error()), err
	}
	if len(request.Ports) > 0 {
		recorder.RecordStartupPhase(contract.StartupPhaseNetworkActivate, time.Since(networkStart))
	}

	succeeded = true
	h.startReadinessWorker(createResponse.ID, request.GetAllocationAttempt(), extraConfig)
	h.startLivenessWorker(createResponse.ID, request.GetAllocationAttempt(), extraConfig)
	h.reportStartRunningStatus(createResponse.ID, request.GetAllocationAttempt(), extraConfig, time.Now().UTC())
	result = contract.StartupResultOK
	resp := startSuccessResponse(createResponse.ID)
	resp.PublishedVolumes = publishResult.Published
	return resp, nil
}

func (h *Controller) configureStartPorts(_ context.Context, containerID, containerIP string, ports []string) error {
	if len(ports) == 0 {
		return nil
	}
	if containerIP == "" {
		return errors.New("Failed to get container IP for DNAT")
	}
	if err := h.sandboxNetworking().SetupDnatRules(containerID, ports, containerIP); err != nil {
		return fmt.Errorf("Failed to setup DNAT rules: %v", err)
	}
	return nil
}

func (h *Controller) deleteManagedContainer(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	unlockLifecycle := h.allocationLifecycleLocks.Lock(request.GetID())
	defer unlockLifecycle()

	h.stopReadinessWorker(request.ID)
	h.stopLivenessWorker(request.ID)
	h.sandboxNetworking().CleanupDnatRules(request.ID)
	_, resource, err := h.deleteContainerRuntime(ctx, &apipb.DeleteContainerRequest{
		ID:      request.ID,
		Timeout: 0,
	})
	runtimeAbsent := isDeleteNotFound(err)
	if err != nil && !runtimeAbsent {
		return new(runtime.DeleteResponse), err
	}
	releaseObservations, err := h.nodeVolumes().Unpublish(ctx, request.ID)
	if err != nil {
		return new(runtime.DeleteResponse), err
	}
	if err := h.releaseAllocationState(request.ID); err != nil {
		return new(runtime.DeleteResponse), err
	}
	finalize := h.finalizeContainerDelete
	if runtimeAbsent {
		// A missing manager/runtime record is the explicit idempotent-delete
		// path. Runtime absence, rather than a monitor that never existed, is
		// the proof used to retire any remaining local claims.
		finalize = h.finalizeFailedContainerDelete
	}
	if err := finalize(request.ID, resource); err != nil {
		return new(runtime.DeleteResponse), err
	}
	_ = os.RemoveAll(filepath.Join(os.TempDir(), "axnoded-secrets", request.ID))
	return &runtime.DeleteResponse{VolumeReleaseObservations: releaseObservations}, nil
}
