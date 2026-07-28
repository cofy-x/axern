package axernsdk

import (
	"context"

	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	"google.golang.org/grpc"
)

func (c *Client) AgentProfileControl() agentprofilev1.AgentProfileControlClient {
	return c.agentProfiles
}
func (c *Client) RolloutControl() rolloutv1.RolloutControlClient { return c.rollouts }
func (c *Client) ArtifactData() artifactv1.ArtifactDataClient    { return c.artifacts }
func (c *Client) CreateRollout(ctx context.Context, request *rolloutv1.CreateRolloutRequest, options ...grpc.CallOption) (*rolloutv1.Rollout, error) {
	response, err := c.rollouts.CreateRollout(ctx, request, options...)
	if err != nil {
		return nil, err
	}
	return response.GetRollout(), nil
}
func (c *Client) GetRollout(ctx context.Context, id string, options ...grpc.CallOption) (*rolloutv1.GetRolloutResponse, error) {
	return c.rollouts.GetRollout(ctx, &rolloutv1.GetRolloutRequest{RolloutID: id}, options...)
}
func (c *Client) CancelRollout(ctx context.Context, id string, options ...grpc.CallOption) (*rolloutv1.Rollout, error) {
	response, err := c.rollouts.CancelRollout(ctx, &rolloutv1.CancelRolloutRequest{RolloutID: id}, options...)
	if err != nil {
		return nil, err
	}
	return response.GetRollout(), nil
}
func (c *Client) RetryRollout(ctx context.Context, id string, options ...grpc.CallOption) (*rolloutv1.Rollout, error) {
	response, err := c.rollouts.RetryRollout(ctx, &rolloutv1.RetryRolloutRequest{RolloutID: id}, options...)
	if err != nil {
		return nil, err
	}
	return response.GetRollout(), nil
}
