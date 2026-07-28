package service

import (
	"context"
	"fmt"

	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type environmentResolver interface {
	ResolveID(context.Context, appenvironment.ResolveParams) (string, error)
}

type CreateParams struct {
	Namespace         string
	EnvironmentID     string
	Spec              *environmentv1.EnvironmentSpec
	Replicas          int32
	Config            *commonv1.ExecutionConfig
	Labels            map[string]string
	ReadinessProbe    *servicev1.ServiceProbe
	LivenessProbe     *servicev1.ServiceProbe
	AutoscalingPolicy *servicev1.ServiceAutoscalingPolicy
}

func NewWithEnvironment(client ServiceClient, environmentClient appenvironment.EnvironmentClient) Control {
	environments := appenvironment.New(environmentClient)
	return Control{client: client, environments: environments}
}

func (c Control) Create(ctx context.Context, params CreateParams) (*servicev1.CreateServiceResponse, error) {
	environmentID := params.EnvironmentID
	if params.Spec != nil || environmentID == "" {
		if c.environments == nil {
			return nil, fmt.Errorf("environment resolver is required")
		}
		resolvedID, err := c.environments.ResolveID(ctx, appenvironment.ResolveParams{
			EnvironmentID: environmentID,
			Spec:          params.Spec,
		})
		if err != nil {
			return nil, err
		}
		environmentID = resolvedID
	}
	return c.client.CreateService(ctx, &servicev1.CreateServiceRequest{
		Namespace:         params.Namespace,
		EnvironmentID:     environmentID,
		Replicas:          params.Replicas,
		Config:            params.Config,
		Labels:            params.Labels,
		ReadinessProbe:    params.ReadinessProbe,
		LivenessProbe:     params.LivenessProbe,
		AutoscalingPolicy: params.AutoscalingPolicy,
	})
}
