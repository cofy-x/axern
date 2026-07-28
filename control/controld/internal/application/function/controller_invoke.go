package appfunction

import (
	"context"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (c *Controller) InvokeFunction(ctx context.Context, req *functionv1.InvokeFunctionRequest, now time.Time) (*functionv1.InvokeFunctionResponse, error) {
	start, err := c.store.StartInvocation(ctx, functionkernel.InvokeParams{
		Namespace:  req.GetNamespace(),
		Name:       req.GetName(),
		FunctionID: req.GetFunctionID(),
		RevisionID: req.GetRevisionID(),
		Mode:       req.GetMode(),
		Payload:    req.GetPayload(),
		Timeout:    req.GetTimeout(),
		RequestID:  req.GetRequestID(),
		Labels:     req.GetLabels(),
	}, now)
	if err != nil {
		return nil, err
	}
	if !start.Found {
		return nil, functionkernel.NotFound()
	}

	if start.Replay {
		return &functionv1.InvokeFunctionResponse{Invocation: start.Invocation}, nil
	}

	invocation := start.Invocation
	fn := start.Function
	revision := start.Revision
	deployment := start.Deployment

	logrus.WithFields(logrus.Fields{
		"function_id":   fn.GetID(),
		"invocation_id": invocation.GetID(),
		"mode":          req.GetMode().String(),
	}).Info("function invocation started")

	if req.GetMode() == functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC {
		return &functionv1.InvokeFunctionResponse{Invocation: invocation}, nil
	}

	return c.executeSyncInvocation(ctx, fn, revision, deployment, invocation, req, now)
}

func (c *Controller) executeSyncInvocation(
	ctx context.Context,
	fn *functionv1.Function,
	revision *functionv1.FunctionRevision,
	deployment *functionv1.FunctionDeployment,
	invocation *functionv1.FunctionInvocation,
	req *functionv1.InvokeFunctionRequest,
	now time.Time,
) (*functionv1.InvokeFunctionResponse, error) {
	executionCtx, cancel := invocationContext(ctx, fn, req)
	defer cancel()

	var result *functionv1.FunctionResult
	var fnErr *functionv1.FunctionError
	var dispatchErr error
	if prepared, err := c.prepareWorker(executionCtx, fn, revision, deployment, now); err != nil {
		dispatchErr = err
	} else {
		deployment = prepared
		result, fnErr, dispatchErr = c.dispatchInvocation(executionCtx, FunctionInvokeDispatch{
			Function:   fn,
			Revision:   revision,
			Deployment: deployment,
			Invocation: invocation,
			Payload:    req.GetPayload(),
			Timeout:    req.GetTimeout(),
		})
	}
	status := functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED
	message := "function invocation succeeded"
	if dispatchErr != nil || fnErr != nil {
		status = functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_FAILED
		if dispatchErr != nil && grpcstatus.Code(dispatchErr) == codes.DeadlineExceeded {
			status = functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT
		}
		fnErr = normalizeDispatchError(deployment, fnErr, dispatchErr)
		message = fnErr.GetMessage()
		result = nil
	}
	finished, ok, err := c.store.FinishInvocation(ctx, invocation.GetID(), status, result, fnErr, message, time.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}
	return &functionv1.InvokeFunctionResponse{Invocation: finished}, nil
}

func invocationContext(parent context.Context, fn *functionv1.Function, req *functionv1.InvokeFunctionRequest) (context.Context, context.CancelFunc) {
	timeout := 30 * time.Second
	if configured := fn.GetSpec().GetTimeout(); configured != nil && configured.CheckValid() == nil && configured.AsDuration() > 0 {
		timeout = configured.AsDuration()
	}
	if requested := req.GetTimeout(); requested != nil && requested.CheckValid() == nil && requested.AsDuration() > 0 {
		timeout = requested.AsDuration()
	}
	return context.WithTimeout(parent, timeout)
}

func (c *Controller) prepareWorker(
	ctx context.Context,
	fn *functionv1.Function,
	revision *functionv1.FunctionRevision,
	deployment *functionv1.FunctionDeployment,
	now time.Time,
) (*functionv1.FunctionDeployment, error) {
	prepared, err := c.refreshDeploymentFromWorkerService(ctx, deployment)
	if err != nil {
		return prepared, err
	}
	prepared, err = c.ensureWorkerWarm(ctx, fn, revision, prepared, now)
	if err != nil || c.services == nil {
		return prepared, err
	}
	prepared, err = c.refreshDeploymentFromWorkerService(ctx, prepared)
	if err != nil || workerDeploymentReady(prepared) {
		return prepared, err
	}

	if c.serviceWatch == nil {
		return prepared, grpcstatus.Error(codes.FailedPrecondition, "function worker readiness watch is not configured")
	}
	service, ok, err := c.services.Get(ctx, prepared.GetWorkerServiceID())
	if err != nil {
		return prepared, err
	}
	if !ok || service == nil {
		return prepared, grpcstatus.Error(codes.FailedPrecondition, "function worker service was not found")
	}
	watch, err := c.serviceWatch.Watch(ctx, service.GetID(), service.GetVersion())
	if err != nil {
		return prepared, err
	}
	defer watch.Close()
	for {
		service, err = watch.Next(ctx)
		if err != nil {
			return prepared, err
		}
		prepared = projectDeploymentFromWorkerService(prepared, service)
		if workerDeploymentReady(prepared) {
			return prepared, nil
		}
		if prepared.GetStatus() == functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_FAILED {
			return prepared, grpcstatus.Errorf(codes.FailedPrecondition, "function worker failed: %s", prepared.GetMessage())
		}
	}
}

func workerDeploymentReady(deployment *functionv1.FunctionDeployment) bool {
	return deployment != nil &&
		deployment.GetStatus() == functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY &&
		deployment.GetDesiredReplicas() > 0 &&
		deployment.GetReadyReplicas() >= deployment.GetDesiredReplicas()
}

func (c *Controller) GetFunctionInvocation(ctx context.Context, req *functionv1.GetFunctionInvocationRequest) (*functionv1.GetFunctionInvocationResponse, error) {
	invocation, ok, err := c.store.GetInvocation(ctx, req.GetInvocationID())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}
	return &functionv1.GetFunctionInvocationResponse{Invocation: invocation}, nil
}

func (c *Controller) ListFunctionInvocations(ctx context.Context, req *functionv1.ListFunctionInvocationsRequest) (*functionv1.ListFunctionInvocationsResponse, error) {
	invocations, nextCursor, err := c.store.ListInvocations(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &functionv1.ListFunctionInvocationsResponse{Invocations: invocations, NextCursor: nextCursor}, nil
}

func (c *Controller) ListFunctionEvents(ctx context.Context, req *functionv1.ListFunctionEventsRequest) (*functionv1.ListFunctionEventsResponse, error) {
	events, err := c.store.ListEvents(ctx, req.GetFunctionID(), req.GetInvocationID(), req.GetRevisionID(), req.GetLimit())
	if err != nil {
		return nil, err
	}
	return &functionv1.ListFunctionEventsResponse{Events: events}, nil
}

func (c *Controller) dispatchInvocation(ctx context.Context, req FunctionInvokeDispatch) (*functionv1.FunctionResult, *functionv1.FunctionError, error) {
	if req.Deployment == nil || req.Deployment.GetWorkerServiceID() == "" {
		return nil, workerUnavailableError(req.Deployment), nil
	}
	if c.invoker == nil {
		return nil, workerDispatchUnavailableError(req.Deployment), nil
	}
	return c.invoker.InvokeFunctionWorker(ctx, req)
}

func normalizeDispatchError(deployment *functionv1.FunctionDeployment, fnErr *functionv1.FunctionError, dispatchErr error) *functionv1.FunctionError {
	if fnErr != nil {
		return fnErr
	}
	if dispatchErr == nil {
		return nil
	}
	if grpcstatus.Code(dispatchErr) == codes.DeadlineExceeded {
		return &functionv1.FunctionError{
			Code:    "timeout",
			Message: dispatchErr.Error(),
			Type:    "Timeout",
		}
	}
	if grpcstatus.Code(dispatchErr) == codes.Unavailable {
		return workerNotReadyError(deployment)
	}
	if grpcstatus.Code(dispatchErr) == codes.ResourceExhausted {
		return &functionv1.FunctionError{
			Code:    "payload_too_large",
			Type:    "PayloadTooLarge",
			Message: "function invocation payload or response exceeds gateway limits",
			Details: workerErrorDetails(deployment),
		}
	}
	return &functionv1.FunctionError{
		Code:    "worker_dispatch_failed",
		Type:    "WorkerDispatchFailed",
		Message: dispatchErr.Error(),
		Details: workerErrorDetails(deployment),
	}
}

func workerUnavailableError(deployment *functionv1.FunctionDeployment) *functionv1.FunctionError {
	if deployment == nil || deployment.GetWorkerServiceID() == "" {
		return &functionv1.FunctionError{
			Code:    "worker_unavailable",
			Type:    "WorkerUnavailable",
			Message: "function worker service is not available",
		}
	}
	return nil
}

func workerNotReadyError(deployment *functionv1.FunctionDeployment) *functionv1.FunctionError {
	return &functionv1.FunctionError{
		Code:    "worker_not_ready",
		Type:    "WorkerNotReady",
		Message: "function worker service is not ready",
		Details: workerErrorDetails(deployment),
	}
}

func workerDispatchUnavailableError(deployment *functionv1.FunctionDeployment) *functionv1.FunctionError {
	if deployment.GetStatus() != functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY {
		return workerNotReadyError(deployment)
	}
	return &functionv1.FunctionError{
		Code:    "worker_dispatch_unavailable",
		Type:    "WorkerDispatchUnavailable",
		Message: "function worker dispatch is not connected",
		Details: workerErrorDetails(deployment),
	}
}

func workerErrorDetails(deployment *functionv1.FunctionDeployment) map[string]string {
	if deployment == nil {
		return nil
	}
	return map[string]string{
		"worker_service_id": deployment.GetWorkerServiceID(),
		"deployment_status": deployment.GetStatus().String(),
	}
}
