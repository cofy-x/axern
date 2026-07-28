package function

import (
	"context"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

type DeployParams struct {
	Namespace   string
	Name        string
	Runtime     string
	Handler     string
	Initializer string
	Labels      map[string]string

	TimeoutSeconds int
	Scaling        *ScalingParams

	Env           map[string]string
	Resources     *commonv1.ResourceSpec
	VolumeMounts  []*commonv1.ServiceVolumeMount
	EnvironmentID string
	Environment   *environmentv1.EnvironmentSpec

	BundleURI    string
	BundleDigest string
	BundleSize   int64
}

type ScalingParams struct {
	MinReplicas int
	MaxReplicas int
	Concurrency int
	IdleSeconds int
}

func (c Control) Deploy(ctx context.Context, params DeployParams) (*functionv1.DeployFunctionResponse, error) {
	spec := &functionv1.FunctionSpec{
		Runtime:     params.Runtime,
		Handler:     params.Handler,
		Initializer: params.Initializer,
	}
	switch {
	case params.EnvironmentID != "":
		spec.WorkerSource = &functionv1.FunctionWorkerSource{
			Source: &functionv1.FunctionWorkerSource_EnvironmentID{EnvironmentID: params.EnvironmentID},
		}
	case params.Environment != nil:
		spec.WorkerSource = &functionv1.FunctionWorkerSource{
			Source: &functionv1.FunctionWorkerSource_Environment{Environment: params.Environment},
		}
	}
	if params.TimeoutSeconds > 0 {
		spec.Timeout = &durationpb.Duration{Seconds: int64(params.TimeoutSeconds)}
	}
	if params.Scaling != nil {
		spec.Scaling = &functionv1.FunctionScalingSpec{
			MinReplicas: int32(params.Scaling.MinReplicas),
			MaxReplicas: int32(params.Scaling.MaxReplicas),
			Concurrency: int32(params.Scaling.Concurrency),
		}
		if params.Scaling.IdleSeconds > 0 {
			spec.Scaling.IdleTimeout = &durationpb.Duration{Seconds: int64(params.Scaling.IdleSeconds)}
		}
	}
	if len(params.Env) > 0 || params.Resources != nil || len(params.VolumeMounts) > 0 {
		config := &commonv1.ExecutionConfig{}
		if len(params.Env) > 0 {
			config.Env = params.Env
		}
		if params.Resources != nil {
			config.Resources = params.Resources
		}
		if len(params.VolumeMounts) > 0 {
			config.VolumeMounts = params.VolumeMounts
		}
		spec.Config = config
	}

	req := &functionv1.DeployFunctionRequest{
		Namespace: params.Namespace,
		Name:      params.Name,
		Spec:      spec,
		Source: &functionv1.FunctionSource{
			Source: &functionv1.FunctionSource_Bundle{
				Bundle: &functionv1.FunctionBundleSource{
					Digest:     params.BundleDigest,
					MediaType:  "application/vnd.axern.function.tar",
					SizeBytes:  params.BundleSize,
					StorageUri: params.BundleURI,
				},
			},
		},
		Labels: params.Labels,
	}

	return c.client.DeployFunction(ctx, req)
}
