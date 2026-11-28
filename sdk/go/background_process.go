package axernsdk

import (
	"context"
	"time"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

// BackgroundProcessOptions selects a finite, unattended process on an active Allocation.
type BackgroundProcessOptions struct {
	Env       map[string]string
	Cwd       string
	User      string
	CallerRef string
	Timeout   time.Duration
}

// StartBackgroundProcess starts a command whose lifetime is independent of this client.
// Use CallerRef to find accepted work if the response is lost.
func (n *AllocationClient) StartBackgroundProcess(ctx context.Context, command any, options BackgroundProcessOptions) (*nodesandboxv1.BackgroundProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	argv, err := normalizeCommand(command)
	if err != nil {
		return nil, err
	}
	if options.Timeout < time.Second || options.Timeout > time.Hour || options.Timeout%time.Second != 0 {
		return nil, &ValidationError{Field: "timeout", Message: "requires a whole number of seconds from 1 through 3600"}
	}
	response, err := n.client.nodes.StartBackgroundProcess(ctx, &nodesandboxv1.StartBackgroundProcessRequest{
		AllocationID: n.allocationID, CallerRef: options.CallerRef,
		Spec: &nodesandboxv1.ExecSpec{
			Argv: argv, Env: options.Env, Cwd: options.Cwd, User: options.User,
			TimeoutSeconds: int64(options.Timeout / time.Second),
		},
	})
	return response.GetProcess(), mapRPCError(err, "start background process", n.allocationID)
}

func (n *AllocationClient) GetBackgroundProcess(ctx context.Context, processID string) (*nodesandboxv1.BackgroundProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	response, err := n.client.nodes.GetBackgroundProcess(ctx,
		&nodesandboxv1.GetBackgroundProcessRequest{AllocationID: n.allocationID, ProcessID: processID})
	return response.GetProcess(), mapRPCError(err, "get background process", n.allocationID)
}

func (n *AllocationClient) ListBackgroundProcesses(ctx context.Context) ([]*nodesandboxv1.BackgroundProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	response, err := n.client.nodes.ListBackgroundProcesses(ctx,
		&nodesandboxv1.ListBackgroundProcessesRequest{AllocationID: n.allocationID})
	return response.GetProcesses(), mapRPCError(err, "list background processes", n.allocationID)
}

func (n *AllocationClient) CancelBackgroundProcess(ctx context.Context, processID string) (*nodesandboxv1.BackgroundProcess, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	response, err := n.client.nodes.CancelBackgroundProcess(ctx,
		&nodesandboxv1.CancelBackgroundProcessRequest{AllocationID: n.allocationID, ProcessID: processID})
	return response.GetProcess(), mapRPCError(err, "cancel background process", n.allocationID)
}
