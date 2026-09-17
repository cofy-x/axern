package allocation

import (
	"context"
	"errors"
	"fmt"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"os"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/protobuf/proto"
)

func (h *Controller) ensurePreparedEnvironment(ctx context.Context, fr *runtime.ResolvedEnvironment) (*environmentcache.PreparedEnvironment, EnvironmentPrepareSummary, error) {
	rootfsCfg, err := environmentcache.RootfsConfigFromResolvedEnvironment(fr)
	if err != nil {
		return nil, EnvironmentPrepareSummary{RootfsType: RootfsTypeFromResolvedEnvironment(fr)}, err
	}
	return h.prepareEnvironment(ctx, fr, rootfsCfg)
}

func (h *Controller) ensurePreparedEnvironmentFromRequest(ctx context.Context, request *runtime.StartRequest) (*environmentcache.PreparedEnvironment, EnvironmentPrepareSummary, error) {
	_, span := sdkobs.Start(ctx, sandboxobs.SpanRootFSPrepare,
		attribute.String(sdkobs.AttrAllocationID, request.GetAllocationID()),
		attribute.String(sdkobs.AttrRuntime, config.RuntimeNameRunsc),
		attribute.String(sdkobs.AttrRootFSType, RootfsTypeFromResolvedEnvironment(request.GetEnvironment())),
	)
	defer span.End()
	rootfsCfg, err := startplan.RootfsConfigFromStartRequest(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rootfs config")
		return nil, EnvironmentPrepareSummary{RootfsType: RootfsTypeFromResolvedEnvironment(request.GetEnvironment())}, err
	}
	fr := request.GetEnvironment()
	lrt, summary, err := h.prepareEnvironment(ctx, fr, rootfsCfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "prepare environment")
		span.SetAttributes(attribute.String(sdkobs.AttrResult, "error"))
	} else {
		span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"), attribute.Bool("axern.runtime_reused", summary.RuntimeReused))
	}
	return lrt, summary, err
}

func (h *Controller) prepareEnvironment(ctx context.Context, fr *runtime.ResolvedEnvironment, rootfsCfg environmentcache.RootfsConfig) (*environmentcache.PreparedEnvironment, EnvironmentPrepareSummary, error) {
	summary := EnvironmentPrepareSummary{
		RootfsType: RootfsTypeFromResolvedEnvironment(fr),
	}
	resolveStart := time.Now()
	resolvedRootfsCfg, err := h.environmentCache.ResolveRootfsConfig(rootfsCfg)
	if err != nil {
		return nil, summary, err
	}
	summary.Steps = append(summary.Steps, StartupStepSample{
		Phase:    contract.StartupPhaseRootfsPrepare,
		Step:     contract.StartupStepRootfsResolve,
		Duration: startupObservationDurationSince(resolveStart),
	})
	lookupStart := time.Now()
	lrt := h.environmentCache.FindReusableEnvironment(fr, resolvedRootfsCfg)
	summary.EnvironmentLookupTime = time.Since(lookupStart)
	if lrt != nil {
		summary.RuntimeReused = true
		return lrt, summary, nil
	}
	prepareStart := time.Now()
	result, err := h.environmentCache.PrepareEnvironment(ctx, fr, resolvedRootfsCfg)
	summary.RootfsPrepareTime = time.Since(prepareStart)
	summary.Steps = append(summary.Steps, startupStepSamplesFromRootfsReport(result.RootfsReport)...)
	summary.RuntimeReused = !result.Created
	return result.Environment, summary, err
}

func startupStepSamplesFromRootfsReport(report environmentcache.RootfsPrepareReport) []StartupStepSample {
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

func startupObservationDurationSince(started time.Time) time.Duration {
	duration := time.Since(started)
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}

func (h *Controller) cleanupFailedStart(ctx context.Context, containerID string) error {
	return h.cleanupFailedStartWithResource(ctx, containerID, container.OccupiedResource{}, false)
}

func (h *Controller) cleanupPersistedFailedStart(ctx context.Context, containerID string) error {
	return h.cleanupFailedStartWithResource(ctx, containerID, container.OccupiedResource{}, true)
}

func (h *Controller) cleanupFailedStartWithResource(ctx context.Context, containerID string, reserved container.OccupiedResource, persistedRecovery bool) error {
	var resource container.OccupiedResource
	resourceKnown := false
	if _, err := h.containers().Get(containerID); err == nil {
		var deleteErr error
		_, resource, deleteErr = h.deleteContainerRuntime(ctx, &apipb.DeleteContainerRequest{ID: containerID, Timeout: 0})
		if deleteErr != nil && !isDeleteNotFound(deleteErr) {
			return fmt.Errorf("delete failed-start runtime: %w", deleteErr)
		}
		resourceKnown = true
	} else if _, ok := h.runtimeMapping(containerID); ok {
		// OCI create may succeed before container metadata is durably indexed.
		// Recover ownership from the allocation record and bundle so a metadata
		// persistence failure cannot strand runtime, network, or resource state.
		handler := h.runscHandler
		if handler == nil {
			return errors.New("resolve partial failed-start runtime: runsc handler unavailable")
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
	if h.allocationHasEgressPolicy(containerID) {
		if err := h.deleteEgressPolicy(ctx, containerID); err != nil {
			return fmt.Errorf("retire failed-start egress policy: %w", err)
		}
	}
	if err := h.releaseAllocationState(containerID, persistedRecovery); err != nil {
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
	return &runtime.StartResponse{}
}

func startSuccessResponse(containerID string) *runtime.StartResponse {
	return &runtime.StartResponse{AllocationID: containerID}
}

func (h *Controller) existingActiveStartResponse(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, bool, error) {
	containerID := request.GetAllocationID()
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
	return resp, true, nil
}

func (h *Controller) startAllocation(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	if err := startplan.ValidateStartRequest(request); err != nil {
		return startErrorResponse(err.Error()), err
	}
	unlockLifecycle := h.allocationLifecycleLocks.Lock(request.GetAllocationID())
	defer unlockLifecycle()
	return h.startAllocationWithLifecycleHeld(ctx, request)
}

func (h *Controller) startAllocationWithLifecycleHeld(ctx context.Context, request *runtime.StartRequest) (response *runtime.StartResponse, returnErr error) {
	if resp, ok, err := h.existingActiveStartResponse(ctx, request); ok {
		return resp, err
	}

	recorder := NewStartMetricsRecorder(h.startMetricSink, config.RuntimeNameRunsc, RootfsTypeFromResolvedEnvironment(request.Environment))
	result := contract.StartupResultError
	succeeded := false
	stateCommitted := false
	resourceReserved := false
	egressPrepared := false
	var reservedResource container.OccupiedResource
	defer func() {
		recorder.Finish(result)
		if succeeded {
			return
		}
		if stateCommitted {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := h.cleanupFailedStartWithResource(cleanupCtx, request.GetAllocationID(), reservedResource, false); err != nil {
				returnErr = errors.Join(returnErr, errRuntimeCleanupPending, fmt.Errorf("ordered failed-start cleanup: %w", err))
			}
			return
		}
		if egressPrepared {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.deleteEgressPolicy(cleanupCtx, request.GetAllocationID()); err != nil {
				returnErr = errors.Join(returnErr, errRuntimeCleanupPending, err)
				return
			}
		}
		if resourceReserved {
			if err := h.containers().Release(reservedResource); err != nil {
				returnErr = errors.Join(returnErr, errRuntimeCleanupPending, fmt.Errorf("retire failed-start resources: %w", err))
			}
		}
	}()

	traceID, _ := trace.GetContextID(ctx)
	resourceAllocateStart := time.Now()
	handler, resource, err := h.prepareContainerResources(
		ctx,
		traceID.String(),
		request.GetAllocationID(),
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
	egressPolicyStart := time.Now()
	egressPrepared, err = h.prepareEgressPolicy(ctx, request, resource)
	if recorder != nil {
		recorder.RecordStartupPhase(contract.StartupPhaseEgressPolicyPrepare, startupObservationDurationSince(egressPolicyStart))
	}
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed egress policy admission: %v", err)), err
	}
	secretMounts, secretCleanup, err := startplan.MaterializeResolvedSecretFiles(request)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to materialize secrets: %v", err)), err
	}
	defer func() {
		if !succeeded {
			secretCleanup()
		}
	}()
	imageMounts, imageMountCleanup, err := h.resolveImageMounts(request)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to resolve image mounts: %v", err)), err
	}
	runtimeRequest := proto.Clone(request).(*runtime.StartRequest)
	runtimeRequest.Mounts = append(runtimeRequest.Mounts, secretMounts...)
	runtimeRequest.Mounts = append(runtimeRequest.Mounts, imageMounts...)
	defer func() {
		if !succeeded && !stateCommitted {
			imageMountCleanup()
		}
	}()
	lrt, prepareSummary, err := h.ensurePreparedEnvironmentFromRequest(ctx, request)
	recorder.SetStartClass(prepareSummary.StartClass())
	recorder.SetRootfsType(prepareSummary.RootfsType)
	recorder.RecordStartupPhase(contract.StartupPhaseEnvironmentLookup, prepareSummary.EnvironmentLookupTime)
	recorder.RecordStartupPhase(contract.StartupPhaseRootfsPrepare, prepareSummary.RootfsPrepareTime)
	for _, sample := range prepareSummary.Steps {
		recorder.RecordStartupStep(sample.Phase, sample.Step, sample.Duration)
	}
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to add new runtime: %v", request.Environment)), err
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
	if err := h.rememberContainerRuntime(request.GetAllocationID(), lrt); err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to persist allocation state: %v", err)), err
	}
	stateCommitted = true

	createRequest := startplan.BuildCreateContainerRequest(
		lrt,
		runtimeRequest,
		startplan.BuildStartEnv(lrt, runtimeRequest),
	)
	templateRequest := startplan.BuildBundleTemplateRequest(lrt, request)

	createResponse, _, err := h.createAllocation(ctx, lrt, request, templateRequest, createRequest, handler, reservedResource, recorder)
	if err != nil {
		return startErrorResponse(fmt.Sprintf("Failed to start: %v", err)), err
	}

	succeeded = true
	h.reportStartRunningStatus(createResponse.ID, time.Now().UTC())
	result = contract.StartupResultOK
	resp := startSuccessResponse(createResponse.ID)
	return resp, nil
}

func (h *Controller) deleteAllocation(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	unlockLifecycle := h.allocationLifecycleLocks.Lock(request.GetID())
	defer unlockLifecycle()
	return h.deleteAllocationWithLifecycleHeld(ctx, request)
}

func (h *Controller) deleteAllocationWithLifecycleHeld(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	if outputSealing := request.GetOutputSealing(); outputSealing != nil {
		expiry := time.Unix(0, outputSealing.GetExpiresAtUnixNano()).UTC()
		if outputSealing.GetExpiresAtUnixNano() <= 0 {
			return new(runtime.DeleteResponse), fmt.Errorf("output sealing expiry is required: %w", errord.ErrInvalidArgument)
		}
		if time.Now().Before(expiry) {
			if err := startplan.ValidateDeclaredOutputs(outputSealing.GetOutputs()); err != nil {
				return new(runtime.DeleteResponse), err
			}
			declarations, localStatePresent, err := h.cleanupDeclaredOutputs(request.GetID(), outputSealing.GetOutputs())
			if err != nil {
				return new(runtime.DeleteResponse), err
			}
			contractDigest, err := declaredOutputContractSHA256(declarations)
			if err != nil {
				return new(runtime.DeleteResponse), err
			}
			target, loadErr := h.containers().Get(request.GetID())
			sources := allocationoutput.Sources{Terminal: true}
			capture := func(_ context.Context, _ string) ([]allocationoutput.Entry, error) {
				return unavailableDeclaredOutputs(declarations, time.Now().UTC()), nil
			}
			if loadErr == nil && target.Metadata != nil && localStatePresent {
				if err := h.quiesceAllocationForOutput(ctx, request, target); err != nil {
					return new(runtime.DeleteResponse), err
				}
				sources.Stdout = target.Metadata.GetStdout()
				sources.Stderr = target.Metadata.GetStderr()
				capture = h.captureDeclaredOutputs(request.GetID(), declarations)
			}
			// Publishing the immutable manifest is the cleanup barrier. A retry sees
			// the same manifest and can continue runtime deletion without recapturing.
			if err := h.outputRetention.Seal(ctx, request.GetID(), expiry, contractDigest, sources, capture); err != nil {
				return new(runtime.DeleteResponse), fmt.Errorf("seal allocation output: %w", err)
			}
		}
	}
	_, resource, err := h.deleteContainerRuntime(ctx, &apipb.DeleteContainerRequest{
		ID:      request.ID,
		Timeout: 0,
	})
	runtimeAbsent := isDeleteNotFound(err)
	if err != nil && !runtimeAbsent {
		return new(runtime.DeleteResponse), err
	}
	// Runtime deletion releases the secret bind mounts. Remove their host-side
	// plaintext before retiring the allocation's durable recovery state, so a
	// cleanup failure remains retryable through the same Allocation identity.
	if err := startplan.CleanupResolvedSecretFiles(request.ID); err != nil {
		return new(runtime.DeleteResponse), fmt.Errorf("cleanup allocation secret files: %w", err)
	}
	if h.allocationHasEgressPolicy(request.ID) {
		if err := h.deleteEgressPolicy(ctx, request.ID); err != nil {
			return new(runtime.DeleteResponse), err
		}
	}
	if err := h.releaseAllocationState(request.ID, false); err != nil {
		return new(runtime.DeleteResponse), err
	}
	finalize := h.finalizeContainerDelete
	if runtimeAbsent {
		// A missing manager/runtime record is the explicit idempotent-delete
		// path. Runtime absence, rather than a monitor that never existed, is
		// the fact used to retire any remaining local claims.
		finalize = h.finalizeFailedContainerDelete
	}
	if err := finalize(request.ID, resource); err != nil {
		return new(runtime.DeleteResponse), err
	}
	return &runtime.DeleteResponse{}, nil
}

func (h *Controller) quiesceAllocationForOutput(ctx context.Context, request *runtime.DeleteRequest, target *container.Container) error {
	if target.Status != nil && target.Status.Get().State() == runtime.ContainerState_CONTAINER_EXITED {
		return nil
	}
	handler := h.runscHandler
	if handler == nil {
		return fmt.Errorf("quiesce allocation for output: runtime unavailable: %w", errord.ErrUnavailable)
	}
	if _, err := handler.KillContainer(ctx, &apipb.SignalContainerRequest{ID: request.GetID(), Signal: "KILL"}, contract.HandlerOptions{ContainerID: request.GetID()}); err != nil && !isDeleteNotFound(err) {
		return fmt.Errorf("quiesce allocation for output: %w", err)
	}
	waitSeconds := request.GetTimeout()
	if waitSeconds <= 0 {
		waitSeconds = 10
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(waitSeconds)*time.Second)
	defer cancel()
	if _, err := handler.Wait(waitCtx, contract.HandlerOptions{ContainerID: request.GetID()}); err != nil && !contract.IsExitStatusUnavailable(err) && !isDeleteNotFound(err) {
		return fmt.Errorf("wait for allocation output barrier: %w", err)
	}
	return nil
}
