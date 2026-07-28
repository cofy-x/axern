package appfunction

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type workerContract struct {
	Command    []string
	PortName   string
	Port       int32
	HealthPath string
	InvokePath string
}

func workerRuntimeContract(runtime string) (workerContract, error) {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "python3.11", "python311":
		return workerContract{
			Command:    []string{"python3", "-m", "axern_sdk.function.worker"},
			PortName:   "function-http",
			Port:       functionWorkerPort,
			HealthPath: "/healthz",
			InvokePath: "/invoke",
		}, nil
	default:
		return workerContract{}, grpcstatus.Errorf(codes.FailedPrecondition, "function runtime %q does not have a worker runtime template", strings.TrimSpace(runtime))
	}
}

func needsWorkerRollout(deployment *functionv1.FunctionDeployment) bool {
	if deployment == nil {
		return true
	}
	return deployment.GetWorkerServiceID() == "" || deployment.GetStatus() == functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_PENDING
}

func (c *Controller) rolloutWorker(ctx context.Context, fn *functionv1.Function, revision *functionv1.FunctionRevision, deployment *functionv1.FunctionDeployment, now time.Time) (*functionv1.Function, *functionv1.FunctionDeployment, error) {
	if c.environments == nil || c.services == nil {
		return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "function worker rollout is not configured")
	}
	contract, err := workerRuntimeContract(fn.GetSpec().GetRuntime())
	if err != nil {
		return nil, nil, err
	}
	labels := workerLabels(fn, revision)
	env, err := c.resolveWorkerEnvironment(ctx, fn, labels, now)
	if err != nil {
		return nil, nil, err
	}
	desired := desiredWorkerReplicas(fn.GetSpec().GetScaling())
	serviceID := deployment.GetWorkerServiceID()
	if serviceID == "" {
		service, err := c.services.Create(ctx, servicekernel.CreateParams{
			Namespace:      fn.GetNamespace(),
			EnvironmentID:  env.GetID(),
			Replicas:       desired,
			Config:         c.workerConfig(fn, revision, contract),
			Labels:         labels,
			ReadinessProbe: workerHTTPProbe(contract.Port, contract.HealthPath),
			LivenessProbe:  workerHTTPProbe(contract.Port, contract.HealthPath),
		}, now)
		if err != nil {
			return nil, nil, err
		}
		serviceID = service.GetID()
	} else {
		service, err := c.services.Update(ctx, workerServiceUpdateRequest(serviceID, env.GetID(), desired, c.workerConfig(fn, revision, contract), labels, contract), now)
		if err != nil {
			return nil, nil, err
		}
		if service == nil {
			return nil, nil, grpcstatus.Error(codes.FailedPrecondition, "function worker service not found")
		}
	}
	rolledFunction, rolledDeployment, ok, err := c.store.AttachWorkerService(ctx, fn.GetID(), revision.GetID(), serviceID, desired, now)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, functionkernel.NotFound()
	}
	if rolledDeployment == nil {
		rolledDeployment = deployment
	}
	return rolledFunction, rolledDeployment, nil
}

func (c *Controller) resolveWorkerEnvironment(ctx context.Context, fn *functionv1.Function, labels map[string]string, now time.Time) (*environmentv1.Environment, error) {
	source := fn.GetSpec().GetWorkerSource()
	if source == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker source is required")
	}

	var env *environmentv1.Environment
	var err error
	switch typed := source.GetSource().(type) {
	case *functionv1.FunctionWorkerSource_EnvironmentID:
		env, err = c.environments.GetEnvironment(ctx, strings.TrimSpace(typed.EnvironmentID))
	case *functionv1.FunctionWorkerSource_Environment:
		if typed.Environment == nil {
			return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker environment is required")
		}
		spec := proto.Clone(typed.Environment).(*environmentv1.EnvironmentSpec)
		if namespace := strings.TrimSpace(spec.GetNamespace()); namespace != "" && namespace != fn.GetNamespace() {
			return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker environment namespace does not match function namespace")
		}
		spec.Namespace = fn.GetNamespace()
		env, err = c.environments.CreateEnvironment(ctx, spec, labels, now)
	default:
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker source is required")
	}
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker environment was not found")
	}
	if env.GetNamespace() != fn.GetNamespace() {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker environment namespace does not match function namespace")
	}
	if env.GetStatus() != environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "function worker environment is not ready: %s", env.GetStatus().String())
	}
	return env, nil
}

func workerServiceUpdateRequest(serviceID, environmentID string, replicas int32, config *commonv1.ExecutionConfig, labels map[string]string, contract workerContract) *servicev1.UpdateServiceRequest {
	return &servicev1.UpdateServiceRequest{
		ServiceID:      serviceID,
		Replicas:       &replicas,
		EnvironmentID:  &environmentID,
		Config:         config,
		Labels:         labels,
		ReadinessProbe: workerHTTPProbe(contract.Port, contract.HealthPath),
		LivenessProbe:  workerHTTPProbe(contract.Port, contract.HealthPath),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
			"replicas",
			"environment_id",
			"config",
			"labels",
			"readiness_probe",
			"liveness_probe",
		}},
	}
}

func workerServiceScaleRequest(serviceID string, replicas int32, config *commonv1.ExecutionConfig, labels map[string]string, contract workerContract) *servicev1.UpdateServiceRequest {
	return &servicev1.UpdateServiceRequest{
		ServiceID:      serviceID,
		Replicas:       &replicas,
		Config:         config,
		Labels:         labels,
		ReadinessProbe: workerHTTPProbe(contract.Port, contract.HealthPath),
		LivenessProbe:  workerHTTPProbe(contract.Port, contract.HealthPath),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
			"replicas",
			"config",
			"labels",
			"readiness_probe",
			"liveness_probe",
		}},
	}
}

func (c *Controller) workerConfig(fn *functionv1.Function, revision *functionv1.FunctionRevision, contract workerContract) *commonv1.ExecutionConfig {
	return workerConfig(fn, revision, contract, c.bundleBaseURL, c.bundleToken)
}

func workerConfig(fn *functionv1.Function, revision *functionv1.FunctionRevision, contract workerContract, bundleBaseURL, bundleToken string) *commonv1.ExecutionConfig {
	var cfg *commonv1.ExecutionConfig
	if fn.GetSpec().GetConfig() != nil {
		cfg = proto.Clone(fn.GetSpec().GetConfig()).(*commonv1.ExecutionConfig)
	} else {
		cfg = &commonv1.ExecutionConfig{}
	}
	cfg.Argv = append([]string(nil), contract.Command...)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	cfg.Env["AXERN_FUNCTION_ID"] = fn.GetID()
	cfg.Env["AXERN_FUNCTION_NAME"] = fn.GetName()
	cfg.Env["AXERN_FUNCTION_NAMESPACE"] = fn.GetNamespace()
	cfg.Env["AXERN_FUNCTION_REVISION_ID"] = revision.GetID()
	cfg.Env["AXERN_FUNCTION_HANDLER"] = fn.GetSpec().GetHandler()
	cfg.Env["AXERN_FUNCTION_INITIALIZER"] = fn.GetSpec().GetInitializer()
	cfg.Env["AXERN_FUNCTION_BUNDLE_URI"] = revision.GetSource().GetBundle().GetStorageUri()
	cfg.Env["AXERN_FUNCTION_BUNDLE_DIGEST"] = revision.GetSource().GetBundle().GetDigest()
	if bundleURL := functionBundleURL(bundleBaseURL, revision); bundleURL != "" {
		cfg.Env["AXERN_FUNCTION_BUNDLE_URL"] = bundleURL
	}
	if strings.TrimSpace(bundleToken) != "" {
		cfg.Env["AXERN_FUNCTION_BUNDLE_TOKEN"] = strings.TrimSpace(bundleToken)
	}
	cfg.Env["AXERN_FUNCTION_WORKER_PORT"] = strconv.FormatInt(int64(contract.Port), 10)
	cfg.Env["AXERN_FUNCTION_INVOKE_PATH"] = contract.InvokePath
	cfg.Ports = upsertWorkerPort(cfg.GetPorts(), &commonv1.PortSpec{
		Name:          contract.PortName,
		Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
		ContainerPort: contract.Port,
	})
	return cfg
}

func functionBundleURL(baseURL string, revision *functionv1.FunctionRevision) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	storageURI := strings.TrimSpace(revision.GetSource().GetBundle().GetStorageUri())
	if !strings.HasPrefix(storageURI, functionkernel.FunctionBundleStorageURIPrefix) {
		return ""
	}
	name := strings.TrimPrefix(storageURI, functionkernel.FunctionBundleStorageURIPrefix)
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/runtime/function-bundles/" + url.PathEscape(name)
}

func upsertWorkerPort(ports []*commonv1.PortSpec, workerPort *commonv1.PortSpec) []*commonv1.PortSpec {
	out := make([]*commonv1.PortSpec, 0, len(ports)+1)
	replaced := false
	for _, port := range ports {
		if port == nil {
			continue
		}
		if strings.TrimSpace(port.GetName()) == strings.TrimSpace(workerPort.GetName()) || port.GetContainerPort() == workerPort.GetContainerPort() {
			if !replaced {
				out = append(out, proto.Clone(workerPort).(*commonv1.PortSpec))
				replaced = true
			}
			continue
		}
		out = append(out, proto.Clone(port).(*commonv1.PortSpec))
	}
	if !replaced {
		out = append(out, proto.Clone(workerPort).(*commonv1.PortSpec))
	}
	return out
}

func workerHTTPProbe(port int32, path string) *servicev1.ServiceProbe {
	return &servicev1.ServiceProbe{
		Action: &servicev1.ServiceProbe_Http{Http: &servicev1.HttpProbe{
			Port:   port,
			Path:   path,
			Scheme: servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTP,
		}},
		InitialDelay:     durationpb.New(time.Second),
		Period:           durationpb.New(5 * time.Second),
		Timeout:          durationpb.New(2 * time.Second),
		SuccessThreshold: 1,
		FailureThreshold: 3,
	}
}

func workerLabels(fn *functionv1.Function, revision *functionv1.FunctionRevision) map[string]string {
	labels := map[string]string{}
	for key, value := range fn.GetLabels() {
		labels[key] = value
	}
	labels[functionWorkerLabelOwner] = "function"
	labels[functionWorkerLabelComponent] = "function-worker"
	labels[functionWorkerLabelFunctionID] = fn.GetID()
	labels[functionWorkerLabelName] = fn.GetName()
	labels[functionWorkerLabelRevisionID] = revision.GetID()
	return labels
}

func desiredWorkerReplicas(scaling *functionv1.FunctionScalingSpec) int32 {
	if scaling == nil {
		return 1
	}
	if scaling.GetMinReplicas() < 0 {
		return 0
	}
	return scaling.GetMinReplicas()
}

func (c *Controller) ensureWorkerWarm(ctx context.Context, fn *functionv1.Function, revision *functionv1.FunctionRevision, deployment *functionv1.FunctionDeployment, now time.Time) (*functionv1.FunctionDeployment, error) {
	if deployment == nil || deployment.GetWorkerServiceID() == "" || deployment.GetDesiredReplicas() > 0 {
		return deployment, nil
	}
	if c.services == nil {
		return deployment, nil
	}
	contract, err := workerRuntimeContract(fn.GetSpec().GetRuntime())
	if err != nil {
		return nil, err
	}
	replicas := desiredInvokeWorkerReplicas(fn.GetSpec().GetScaling())
	if replicas <= 0 {
		return deployment, nil
	}
	labels := workerLabels(fn, revision)
	service, err := c.services.Update(ctx, workerServiceScaleRequest(deployment.GetWorkerServiceID(), replicas, c.workerConfig(fn, revision, contract), labels, contract), now)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "function worker service not found")
	}
	_, warmed, ok, err := c.store.AttachWorkerService(ctx, fn.GetID(), revision.GetID(), deployment.GetWorkerServiceID(), replicas, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, functionkernel.NotFound()
	}
	return warmed, nil
}

func desiredInvokeWorkerReplicas(scaling *functionv1.FunctionScalingSpec) int32 {
	if scaling == nil {
		return 1
	}
	if scaling.GetMaxReplicas() < 1 {
		return 0
	}
	return 1
}

func (c *Controller) refreshDeploymentFromWorkerService(ctx context.Context, deployment *functionv1.FunctionDeployment) (*functionv1.FunctionDeployment, error) {
	if deployment == nil || c.services == nil || strings.TrimSpace(deployment.GetWorkerServiceID()) == "" {
		return deployment, nil
	}
	service, ok, err := c.services.Get(ctx, deployment.GetWorkerServiceID())
	if err != nil {
		return nil, err
	}
	if !ok || service == nil {
		return deployment, nil
	}
	return projectDeploymentFromWorkerService(deployment, service), nil
}

func projectDeploymentFromWorkerService(deployment *functionv1.FunctionDeployment, service *servicev1.Service) *functionv1.FunctionDeployment {
	if deployment == nil || service == nil {
		return deployment
	}
	next := proto.Clone(deployment).(*functionv1.FunctionDeployment)
	next.DesiredReplicas = service.GetReplicas()
	next.ReadyReplicas = service.GetReadyReplicas()
	next.Message = service.GetMessage()
	next.DiagnosticCode = service.GetDiagnosticCode()
	switch {
	case next.GetDesiredReplicas() <= 0:
		next.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO
	case service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_FAILED:
		next.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_FAILED
	case next.GetReadyReplicas() >= next.GetDesiredReplicas():
		next.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_READY
	default:
		next.Status = functionv1.FunctionDeploymentStatus_FUNCTION_DEPLOYMENT_STATUS_WARMING
	}
	return next
}
