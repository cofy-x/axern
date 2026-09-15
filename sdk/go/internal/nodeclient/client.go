package nodeclient

import (
	"bytes"
	"context"
	"time"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

type Client struct {
	allocationID string
	nodes        nodesandboxv1.NodeSandboxClient
}

type Options struct {
	Env         map[string]string
	Cwd         string
	Timeout     time.Duration
	User        string
	TTY         bool
	InitialCols uint32
	InitialRows uint32
}

type Result struct {
	ExitCode        int32
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
}

func New(allocationID string, nodes nodesandboxv1.NodeSandboxClient) *Client {
	return &Client{allocationID: allocationID, nodes: nodes}
}

func (c *Client) Exec(ctx context.Context, argv []string, options Options) (Result, error) {
	response, err := c.nodes.Exec(ctx, &nodesandboxv1.ExecRequest{
		AllocationID: c.allocationID,
		Spec: &nodesandboxv1.ExecSpec{
			Argv:           append([]string(nil), argv...),
			Env:            cloneMap(options.Env),
			Cwd:            options.Cwd,
			TimeoutSeconds: int64(options.Timeout.Seconds()),
			User:           options.User,
			Tty:            options.TTY},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExitCode:        response.GetExitCode(),
		Stdout:          bytes.Clone(response.GetStdout()),
		Stderr:          bytes.Clone(response.GetStderr()),
		StdoutTruncated: response.GetStdoutTruncated(),
		StderrTruncated: response.GetStderrTruncated()}, nil
}

func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
