package allocation

import (
	"context"
	"errors"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/sirupsen/logrus"
)

func (h *Controller) deleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest) (response *apipb.DeleteContainerResponse, err error) {
	traceID, spanID := trace.GetContextID(ctx)
	start := time.Now()
	defer func() {
		if err != nil {
			logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("DeleteContainer %s failed, traceID: %v, spanID: %v, err: %v", request.ID, traceID, spanID, err)
		} else {
			logrus.WithField(trace.ContextKeyTraceId, traceID).Infof("DeleteContainer %s success, traceID: %v, spanID: %v, cost: %v", request.ID, traceID, spanID, time.Since(start).String())
		}
	}()

	stageStarted := time.Now()
	c, handler, err := h.runtimeHandlerForContainer(request.ID)
	if err != nil {
		recordAllocationDeleteStage("resolve_runtime", "", stageStarted, err)
		return response, err
	}
	runtimeName := c.Metadata.RuntimeHandler
	recordAllocationDeleteStage("resolve_runtime", runtimeName, stageStarted, nil)

	stageStarted = time.Now()
	if err := h.checkRuntime(runtimeName); err != nil {
		recordAllocationDeleteStage("validate_runtime", runtimeName, stageStarted, err)
		return response, errord.ErrNotImplemented
	}
	recordAllocationDeleteStage("validate_runtime", runtimeName, stageStarted, nil)

	stageStarted = time.Now()
	resource, err := h.containers().CollectResourceByID(request.ID)
	if err != nil {
		recordAllocationDeleteStage("collect_resource", runtimeName, stageStarted, err)
		return response, err
	}
	recordAllocationDeleteStage("collect_resource", runtimeName, stageStarted, nil)

	stageStarted = time.Now()
	response, err = h.deleteContainerWithRuntime(ctx, request, c, handler, traceID.String(), spanID.String())
	if err != nil {
		recordAllocationDeleteStage("runtime_delete", runtimeName, stageStarted, err)
		return response, err
	}
	recordAllocationDeleteStage("runtime_delete", runtimeName, stageStarted, nil)

	stageStarted = time.Now()
	if err := h.sandboxNetworking().CleanupActivationNetwork(resource); err != nil {
		recordAllocationDeleteStage("network_cleanup", runtimeName, stageStarted, err)
		return response, err
	}
	recordAllocationDeleteStage("network_cleanup", runtimeName, stageStarted, nil)
	stageStarted = time.Now()
	h.sandboxNetworking().CloseHTTPProxyTransports(request.ID)
	recordAllocationDeleteStage("transport_cleanup", runtimeName, stageStarted, nil)

	stageStarted = time.Now()
	if err := h.finalizeContainerDelete(request.ID); err != nil {
		recordAllocationDeleteStage("finalize", runtimeName, stageStarted, err)
		return response, err
	}
	recordAllocationDeleteStage("finalize", runtimeName, stageStarted, nil)
	return response, nil
}

func recordAllocationDeleteStage(stage, runtimeName string, started time.Time, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.RecordAllocationDeleteStage(stage, runtimeName, result, time.Since(started).Seconds())
}

func (h *Controller) deleteContainerWithRuntime(
	ctx context.Context,
	request *apipb.DeleteContainerRequest,
	c *container.Container,
	handler contract.RuntimeHandler,
	traceID, spanID string,
) (*apipb.DeleteContainerResponse, error) {
	options := contract.HandlerOptions{
		TraceID:               traceID,
		SpanID:                spanID,
		ContainerID:           request.ID,
		CleanRootDir:          cleanRootDirForContainer(c),
		AdditionalAnnotations: c.Spec.Annotations,
	}

	if request.Timeout == 0 {
		options.ForceDelete = true
		return h.callRuntimeDelete(ctx, request, c.Metadata.RuntimeHandler, handler, options, "force delete container")
	}

	delCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Timeout)*time.Second)
	defer cancel()

	response, err := h.callRuntimeDelete(delCtx, request, c.Metadata.RuntimeHandler, handler, options, "delete container with timeout")
	if err == nil {
		return response, nil
	}

	logrus.WithField(trace.ContextKeyTraceId, traceID).Errorf("runtime handler delete container with timeout %v (seconds) failed: %v, try delete it force", request.Timeout, err)
	options.ForceDelete = true
	options.CleanRootDir = ""
	return h.callRuntimeDelete(ctx, request, c.Metadata.RuntimeHandler, handler, options, "delete container force")
}

func (h *Controller) callRuntimeDelete(
	ctx context.Context,
	request *apipb.DeleteContainerRequest,
	runtimeName string,
	handler contract.RuntimeHandler,
	options contract.HandlerOptions,
	operation string,
) (*apipb.DeleteContainerResponse, error) {
	response, err := handler.DeleteContainer(ctx, request, options)
	if isDeleteNotFound(err) {
		err = nil
	}
	if err != nil {
		metrics.RecordRuntimeCallResult("delete", "failed", runtimeName)
		logrus.WithField(trace.ContextKeyTraceId, options.TraceID).Errorf("runtime handler %s failed: %v", operation, err)
		return response, err
	}

	metrics.RecordRuntimeCallResult("delete", "success", runtimeName)
	return response, nil
}

func isDeleteNotFound(err error) bool {
	return errors.Is(err, errord.ErrNotFound) || errord.IsNotFound(errord.FromGRPC(err))
}

func IsDeleteNotFound(err error) bool {
	return isDeleteNotFound(err)
}

func cleanRootDirForContainer(c *container.Container) string {
	if c == nil || c.Spec == nil || c.Spec.Process == nil || c.Spec.Process.Cwd == "" {
		return ""
	}
	taskUUID := c.EnvValue("TASK_UUID")
	if taskUUID == "" || !strings.Contains(c.Spec.Process.Cwd, taskUUID) {
		return ""
	}
	return c.Spec.Process.Cwd
}

func (h *Controller) finalizeContainerDelete(containerID string) error {
	if err := h.containers().Delete(containerID); err != nil {
		return err
	}
	go h.containers().ReceiveEvent(container.Event{
		Type:        container.EventTypeDelete,
		ContainerID: containerID,
	})
	h.notifyInventoryChanged()
	return nil
}
