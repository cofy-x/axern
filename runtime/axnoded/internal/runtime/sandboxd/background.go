package sandboxd

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (c *Client) StartBackground(ctx context.Context, spec wire.BackgroundProcessSpec) (wire.ProcessStatus, error) {
	status, err := c.StartProcess(ctx, ProcessStartRequest{
		Args: spec.Argv, Cwd: spec.Cwd, Env: processEnvList(spec.Env), User: spec.User,
		Background: true, CallerRef: spec.CallerRef, TimeoutMs: spec.Timeout.Milliseconds(),
	})
	if err == nil && (!status.Background || status.ID == "") {
		return wire.ProcessStatus{}, fmt.Errorf("sandboxd did not acknowledge background process identity: %w", errord.ErrFailedPrecondition)
	}
	return wire.ProcessStatus(status), processOperationError("start background process", err)
}

func (c *Client) GetBackground(ctx context.Context, id string) (wire.ProcessStatus, error) {
	status, err := c.ProcessStatus(ctx, id)
	if err == nil && !status.Background {
		return wire.ProcessStatus{}, errord.ErrNotFound
	}
	return wire.ProcessStatus(status), processOperationError("get background process", err)
}

func (c *Client) ListBackground(ctx context.Context) ([]wire.ProcessStatus, error) {
	// The filtered sandboxd route limits serialization to detached status
	// receipts, which never carry captured output from attached commands.
	var result wire.ProcessListResponse
	err := c.getJSON(ctx, wire.PathBackgroundProcesses, &result)
	return result.Processes, processOperationError("list background processes", err)
}

func (c *Client) CancelBackground(ctx context.Context, id string) (wire.ProcessStatus, error) {
	if _, err := c.GetBackground(ctx, id); err != nil {
		return wire.ProcessStatus{}, err
	}
	if _, err := c.SignalProcess(ctx, id, "KILL"); err != nil {
		return wire.ProcessStatus{}, processOperationError("cancel background process", err)
	}
	status, err := c.WaitProcess(ctx, id)
	return wire.ProcessStatus(status), processOperationError("wait for cancelled background process", err)
}
