package allocation

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

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
	handler, resource, err := h.prepareContainerCreate(ctx, traceID.String(), request)
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
		if cleanupErr := h.containers().Release(resource); cleanupErr != nil {
			return response, "", errors.Join(err, fmt.Errorf("release resources after container create failure: %w", cleanupErr))
		}
		h.cleanupFailedContainerCreate(traceID.String(), resource.ID, metaData)
		return response, "", err
	}

	response.ID = resource.ID
	h.containers().StoreMetadata(resource.ID, metaData)
	if err := h.containers().SetResources(resource.ID, request.Resource, resourceSpec); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Warnf("persist container %s resources failed: %v", resource.ID, err)
	}
	h.syncCreatedContainerStatus(ctx, resource.ID, handler)
	logrus.WithField(trace.ContextKeyTraceId, traceID).Infof("CreateContainer %s success, traceID: %v, spanID: %v, cost: %v", resource.ID, traceID, spanID, time.Since(start).String())

	go h.containers().ReceiveEvent(container.Event{
		Type:        container.EventTypeCreate,
		MetaData:    metaData,
		ContainerID: resource.ID,
	})
	return response, containerIPFromResource(resource), nil
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
		if err := h.containers().SyncStatusFromState(containerID, state); err != nil {
			logrus.Warnf("sync created container %s status failed: %v", containerID, err)
		}
		return
	}
}

func (h *Controller) prepareContainerCreate(ctx context.Context, traceID string, request *apipb.CreateContainerRequest) (contract.RuntimeHandler, container.OccupiedResource, error) {
	var empty container.OccupiedResource

	if err := h.checkRuntime(request.Runtime); err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Debugf("check runtime failed: %v", err)
		return nil, empty, errord.ErrNotImplemented
	}

	handler, err := h.runtimeHandler(request.Runtime)
	if err != nil {
		return nil, empty, errord.ErrNotImplemented
	}

	resourceNames := handler.Requirements().Resources
	if resourceNames == nil {
		resourceNames = []resourcemanager.ResourceName{}
	}

	resource, err := h.containers().Occupy(resourcemanager.AllocateOption{
		Context:      ctx,
		ContainerID:  request.GetID(),
		EnvID:        envValue(request.Envs, config.SandboxEnvKey),
		TraceID:      traceID,
		FunctionName: envValue(request.Envs, config.SandboxFunctionNameKey),
	}, resourceNames...)
	if err != nil {
		logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("occpuy resource failed: %v", err)
		return nil, empty, err
	}

	return handler, resource, nil
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
	if err := h.containers().CleanContainerRoot(containerID); err != nil {
		logrus.WithField("trace_id", traceID).WithError(err).Warn("cleanup failed container root")
	}
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
