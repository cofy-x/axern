package run

import (
	"context"
	"fmt"

	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc"
)

type RunClient interface {
	CreateRun(context.Context, *runv1.CreateRunRequest, ...grpc.CallOption) (*runv1.CreateRunResponse, error)
	GetRun(context.Context, *runv1.GetRunRequest, ...grpc.CallOption) (*runv1.GetRunResponse, error)
	ListRuns(context.Context, *runv1.ListRunsRequest, ...grpc.CallOption) (*runv1.ListRunsResponse, error)
	CancelRun(context.Context, *runv1.CancelRunRequest, ...grpc.CallOption) (*runv1.CancelRunResponse, error)
}

type Control struct {
	client       RunClient
	environments *appenvironment.Control
}

type CreateParams struct {
	Namespace     string
	EnvironmentID string
	Spec          *environmentv1.EnvironmentSpec
	Config        *commonv1.ExecutionConfig
	Labels        map[string]string
}

func New(client RunClient) Control {
	return Control{client: client}
}

func NewWithEnvironment(client RunClient, environmentClient appenvironment.EnvironmentClient) Control {
	environments := appenvironment.New(environmentClient)
	return Control{client: client, environments: &environments}
}

func (c Control) Create(ctx context.Context, params CreateParams) (*runv1.CreateRunResponse, error) {
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
	return c.client.CreateRun(ctx, &runv1.CreateRunRequest{
		Namespace:     params.Namespace,
		EnvironmentID: environmentID,
		Config:        params.Config,
		Labels:        params.Labels,
	})
}

func (c Control) Get(ctx context.Context, runID string) (*runv1.GetRunResponse, error) {
	return c.client.GetRun(ctx, &runv1.GetRunRequest{RunID: runID})
}

func (c Control) List(ctx context.Context, req *runv1.ListRunsRequest) (*runv1.ListRunsResponse, error) {
	return c.client.ListRuns(ctx, req)
}

func (c Control) Cancel(ctx context.Context, runID string) (*runv1.CancelRunResponse, error) {
	return c.client.CancelRun(ctx, &runv1.CancelRunRequest{RunID: runID})
}
