package appfunction

import (
	"context"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func (c *Controller) UploadFunctionBundle(ctx context.Context, params functionkernel.UploadBundleParams, now time.Time) (*functionv1.UploadFunctionBundleResponse, error) {
	bundle, err := c.store.SaveBundle(ctx, params, now)
	if err != nil {
		return nil, err
	}
	return &functionv1.UploadFunctionBundleResponse{Bundle: bundle}, nil
}

func (c *Controller) DeployFunction(ctx context.Context, req *functionv1.DeployFunctionRequest, now time.Time) (*functionv1.DeployFunctionResponse, error) {
	if _, err := workerRuntimeContract(req.GetSpec().GetRuntime()); err != nil {
		return nil, err
	}
	result, err := c.store.DeployFunction(ctx, functionkernel.DeployParams{
		Namespace: req.GetNamespace(),
		Name:      req.GetName(),
		Spec:      req.GetSpec(),
		Source:    req.GetSource(),
		Labels:    req.GetLabels(),
	}, now)
	if err != nil {
		return nil, err
	}
	logrus.WithFields(logrus.Fields{
		"function_id": result.Function.GetID(),
		"revision_id": result.Revision.GetID(),
		"namespace":   result.Function.GetNamespace(),
		"name":        result.Function.GetName(),
	}).Info("function deployed")

	if needsWorkerRollout(result.Deployment) {
		rolledFunction, rolledDeployment, err := c.rolloutWorker(ctx, result.Function, result.Revision, result.Deployment, now)
		if err != nil {
			return nil, err
		}
		result.Function = rolledFunction
		result.Deployment = rolledDeployment
	}
	return &functionv1.DeployFunctionResponse{
		Function:   result.Function,
		Revision:   result.Revision,
		Deployment: result.Deployment,
	}, nil
}

func (c *Controller) GetFunction(ctx context.Context, req *functionv1.GetFunctionRequest) (*functionv1.GetFunctionResponse, error) {
	fn, revision, deployment, ok, err := c.store.GetFunction(ctx, req.GetFunctionID(), req.GetNamespace(), req.GetName())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}
	if fn.GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_DELETING &&
		fn.GetStatus() != functionv1.FunctionStatus_FUNCTION_STATUS_DELETED {
		deployment, err = c.refreshDeploymentFromWorkerService(ctx, deployment)
		if err != nil {
			return nil, err
		}
	}
	return &functionv1.GetFunctionResponse{
		Function:       projectFunctionStatus(fn, deployment),
		ActiveRevision: revision,
		Deployment:     deployment,
	}, nil
}

func projectFunctionStatus(fn *functionv1.Function, deployment *functionv1.FunctionDeployment) *functionv1.Function {
	if fn == nil || deployment == nil ||
		fn.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETED ||
		fn.GetStatus() == functionv1.FunctionStatus_FUNCTION_STATUS_DELETING {
		return fn
	}
	next := proto.Clone(fn).(*functionv1.Function)
	next.DeploymentStatus = deployment.GetStatus()
	next.Message = strings.TrimSpace(deployment.GetMessage())
	next.DiagnosticCode = deployment.GetDiagnosticCode()
	switch deployment.GetStatus() {
	case functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY,
		functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO:
		next.Status = functionv1.FunctionStatus_FUNCTION_STATUS_READY
	case functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_FAILED:
		next.Status = functionv1.FunctionStatus_FUNCTION_STATUS_FAILED
	default:
		next.Status = functionv1.FunctionStatus_FUNCTION_STATUS_DEPLOYING
	}
	return next
}

func (c *Controller) ListFunctions(ctx context.Context, req *functionv1.ListFunctionsRequest) (*functionv1.ListFunctionsResponse, error) {
	functions, nextCursor, err := c.store.ListFunctions(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &functionv1.ListFunctionsResponse{Functions: functions, NextCursor: nextCursor}, nil
}

func (c *Controller) DeleteFunction(ctx context.Context, req *functionv1.DeleteFunctionRequest, now time.Time) (*functionv1.DeleteFunctionResponse, error) {
	_, _, deployment, ok, err := c.store.GetFunction(ctx, req.GetFunctionID(), req.GetNamespace(), req.GetName())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}

	fn, ok, err := c.store.DeleteFunction(ctx, req.GetFunctionID(), req.GetNamespace(), req.GetName(), now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}

	logrus.WithFields(logrus.Fields{
		"function_id": fn.GetID(),
		"namespace":   fn.GetNamespace(),
		"name":        fn.GetName(),
	}).Info("function deleted")

	if workerServiceID := deployment.GetWorkerServiceID(); workerServiceID != "" {
		if c.services != nil {
			if _, _, err := c.services.Delete(ctx, servicekernel.DeleteParams{ServiceID: workerServiceID}, now); err != nil {
				logrus.WithError(err).WithField("worker_service_id", workerServiceID).Warn("failed to delete worker service after function soft-delete")
			}
		}
	}
	return &functionv1.DeleteFunctionResponse{Function: fn}, nil
}
