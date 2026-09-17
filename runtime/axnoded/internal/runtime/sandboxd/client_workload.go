package sandboxd

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (c *Client) StopWorkload(ctx context.Context, signal string) (wire.WorkloadExitResponse, error) {
	var response wire.WorkloadExitResponse
	err := c.postJSON(ctx, wire.PathWorkloadStop, wire.WorkloadStopRequest{Signal: signal}, &response)
	return response, err
}

func (c *Client) SignalWorkload(ctx context.Context, signal string) error {
	return c.postJSON(ctx, wire.PathWorkloadSignal, wire.WorkloadStopRequest{Signal: signal}, nil)
}

func (c *Client) WaitWorkload(ctx context.Context) (wire.WorkloadExitResponse, error) {
	var response wire.WorkloadExitResponse
	err := c.postNoBody(ctx, wire.PathWorkloadWait, &response)
	return response, err
}
