package allocation

import (
	"context"
	"errors"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/sirupsen/logrus"
)

const executionEnvelopePrepareTimeout = 30 * time.Second

func (h *Controller) scheduleExecutionEnvelopePrepare(lrt *langrtmanager.LanguageRuntime) {
	if !h.executionEnvelopeSupported(lrt) || !lrt.Retained() || !lrt.ExecutionEnvelopeEnabled() {
		return
	}
	if !lrt.BeginExecutionEnvelopePrepare() {
		return
	}

	go h.prepareExecutionEnvelopeAsync(lrt)
}

func (h *Controller) executionEnvelopeSupported(lrt *langrtmanager.LanguageRuntime) bool {
	if lrt == nil || lrt.Sandbox == "" {
		return false
	}
	handler, err := h.runtimeHandler(lrt.Sandbox)
	if err != nil {
		return false
	}
	_, ok := handler.(contract.ExecutionEnvelopeHandler)
	return ok
}

func (h *Controller) prepareExecutionEnvelopeAsync(lrt *langrtmanager.LanguageRuntime) {
	runtimeName := ""
	rootfsType := contract.StartupRootfsTypeUnknown
	if lrt != nil {
		runtimeName = lrt.Sandbox
		rootfsType = rootfsTypeFromLanguageRuntime(lrt)
	}

	start := time.Now()
	result := contract.StartupResultError
	defer func() {
		metrics.RecordExecutionEnvelopePrepareDuration(runtimeName, rootfsType, result, time.Since(start).Seconds())
	}()

	if lrt == nil {
		return
	}

	handler, err := h.runtimeHandler(lrt.Sandbox)
	if err != nil {
		lrt.FinishExecutionEnvelopePrepare(nil)
		metrics.RecordExecutionEnvelope(runtimeName, rootfsType, "error")
		return
	}
	envelopeHandler, ok := handler.(contract.ExecutionEnvelopeHandler)
	if !ok {
		lrt.FinishExecutionEnvelopePrepare(nil)
		return
	}

	request := startplan.BuildExecutionEnvelopeRequest(lrt, h.config.NatBackend)
	templateRequest := startplan.BuildBundleTemplateRequestFromLanguageRuntime(lrt)
	traceID := fmt.Sprintf("execution-envelope/%s", lrt.ID)
	ctx, cancel := context.WithTimeout(context.Background(), executionEnvelopePrepareTimeout)
	defer cancel()
	_, resource, err := h.prepareContainerCreate(ctx, traceID, request)
	if err != nil {
		lrt.FinishExecutionEnvelopePrepare(nil)
		metrics.RecordExecutionEnvelope(runtimeName, rootfsType, "error")
		return
	}

	runtimeEnvelope, err := envelopeHandler.PrepareExecutionEnvelope(ctx, request, h.createHandlerOptions(traceID, "", lrt, templateRequest, resource, nil))
	if err != nil {
		lrt.FinishExecutionEnvelopePrepare(nil)
		metrics.RecordExecutionEnvelope(runtimeName, rootfsType, "error")
		_ = h.destroyPreparedExecutionEnvelope(ctx, runtimeName, &langrtmanager.ExecutionEnvelope{
			RuntimeEnvelope: runtimeEnvelope,
			Resource:        resource,
		})
		return
	}

	var envelope *langrtmanager.ExecutionEnvelope
	envelope = &langrtmanager.ExecutionEnvelope{
		RuntimeEnvelope: runtimeEnvelope,
		Resource:        resource,
		PreparedAt:      time.Now().UTC(),
		Destroy: func(ctx context.Context) error {
			return h.destroyPreparedExecutionEnvelope(ctx, runtimeName, envelope)
		},
	}
	if !lrt.FinishExecutionEnvelopePrepare(envelope) {
		metrics.RecordExecutionEnvelope(runtimeName, rootfsType, "error")
		_ = h.destroyPreparedExecutionEnvelope(ctx, runtimeName, envelope)
		return
	}

	result = contract.StartupResultOK
	metrics.RecordExecutionEnvelope(runtimeName, rootfsType, "prepared")
}

func (h *Controller) destroyPreparedExecutionEnvelope(ctx context.Context, runtimeName string, envelope *langrtmanager.ExecutionEnvelope) error {
	if envelope == nil {
		return nil
	}

	var destroyErr error
	if runtimeName != "" && envelope.Resource.ID != "" {
		if handler, err := h.runtimeHandler(runtimeName); err == nil {
			_, destroyErr = handler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{
				ID:      envelope.Resource.ID,
				Timeout: 0,
			}, contract.HandlerOptions{
				ContainerID: envelope.Resource.ID,
				ForceDelete: true,
			})
		} else {
			destroyErr = err
		}
	}
	if len(envelope.Resource.Resources) > 0 || envelope.Resource.ID != "" {
		destroyErr = errors.Join(destroyErr, h.containers().Release(envelope.Resource))
	}
	if destroyErr == nil && envelope.Resource.ID != "" {
		h.containers().CleanContainerRoot(envelope.Resource.ID)
	}
	return destroyErr
}

func (h *Controller) tryStartWithExecutionEnvelope(
	ctx context.Context,
	lrt *langrtmanager.LanguageRuntime,
	request *runtime.StartRequest,
	recorder contract.StartupPhaseRecorder,
) (*apipb.CreateContainerResponse, string, bool, error) {
	handler, err := h.runtimeHandler(lrt.Sandbox)
	if err != nil {
		if envelope := lrt.DiscardExecutionEnvelope(); envelope != nil {
			_ = h.destroyPreparedExecutionEnvelope(context.Background(), lrt.Sandbox, envelope)
		}
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, RootfsTypeFromRuntimeTemplate(request.RuntimeTemplate), "fallback")
		return nil, "", false, nil
	}
	envelopeHandler, ok := handler.(contract.ExecutionEnvelopeHandler)
	if !ok || !envelopeHandler.EligibleForExecutionEnvelope(request) {
		if envelope := lrt.SetExecutionEnvelopeEnabled(false); envelope != nil {
			_ = h.destroyPreparedExecutionEnvelope(context.Background(), lrt.Sandbox, envelope)
		}
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, RootfsTypeFromRuntimeTemplate(request.RuntimeTemplate), "fallback")
		return nil, "", false, nil
	}
	lrt.SetExecutionEnvelopeEnabled(true)

	envelope := lrt.ClaimExecutionEnvelope()
	if envelope == nil {
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "miss")
		return nil, "", false, nil
	}

	if !ok {
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "error")
		_ = h.destroyPreparedExecutionEnvelope(ctx, lrt.Sandbox, envelope)
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "fallback")
		return nil, "", false, nil
	}

	traceID, spanID := trace.GetContextID(ctx)
	activateStart := time.Now()
	metaData, activateErr := envelopeHandler.ActivateExecutionEnvelope(ctx, envelope.RuntimeEnvelope, h.createHandlerOptions(traceID.String(), spanID.String(), lrt, nil, envelope.Resource, recorder))
	activateResult := contract.StartupResultOK
	if activateErr != nil {
		activateResult = contract.StartupResultError
	}
	metrics.RecordExecutionEnvelopeActivateDuration(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), activateResult, time.Since(activateStart).Seconds())
	if activateErr != nil {
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "error")
		_ = h.destroyPreparedExecutionEnvelope(context.Background(), lrt.Sandbox, envelope)
		metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "fallback")
		return nil, "", false, nil
	}

	metrics.RecordExecutionEnvelope(request.RuntimeTemplate.Sandbox, rootfsTypeFromLanguageRuntime(lrt), "hit")
	h.containers().StoreMetadata(envelope.Resource.ID, metaData)
	if err := h.containers().SetResources(envelope.Resource.ID, startplan.ResourcesToLinux(request.Resources), request.Resources); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("persist envelope-activated container %s resources failed: %v", envelope.Resource.ID, err)
	}
	go h.containers().ReceiveEvent(container.Event{
		Type:        container.EventTypeCreate,
		MetaData:    metaData,
		ContainerID: envelope.Resource.ID,
	})
	return &apipb.CreateContainerResponse{ID: envelope.Resource.ID}, containerIPFromResource(envelope.Resource), true, nil
}
