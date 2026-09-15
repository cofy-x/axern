package axernsdk

import (
	"context"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
)

// ExecOptions configures a collected sandbox command execution.
type ExecOptions struct {
	Env     map[string]string
	Cwd     string
	Timeout time.Duration
	User    string
	TTY     bool
	Check   bool
}

// ExecResult contains collected command output and exit status.
type ExecResult struct {
	ExitCode        int32
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
}

// StdoutString returns stdout decoded as a Go string.
func (r ExecResult) StdoutString() string {
	return string(r.Stdout)
}

// StderrString returns stderr decoded as a Go string.
func (r ExecResult) StderrString() string {
	return string(r.Stderr)
}

// AllocationClient provides lower-level operations for an existing allocation.
type AllocationClient struct {
	client       *Client
	allocationID string
}

// Allocation returns a low-level sandbox client for allocationID.
func (c *Client) Allocation(allocationID string) (*AllocationClient, error) {
	if c == nil {
		return nil, requiredError("client")
	}
	if allocationID == "" {
		return nil, requiredError("allocation_id")
	}
	return &AllocationClient{client: c, allocationID: allocationID}, nil
}

// Exec runs a command in the allocation and collects stdout/stderr.
func (n *AllocationClient) Exec(ctx context.Context, command any, options ExecOptions) (ExecResult, error) {
	if err := n.validate(); err != nil {
		return ExecResult{}, err
	}
	if err := validateExecOptions(options); err != nil {
		return ExecResult{}, err
	}
	argv, err := normalizeCommand(command)
	if err != nil {
		return ExecResult{}, err
	}
	result, err := n.rpcClient().Exec(ctx, argv, nodeclient.Options{
		Env:     options.Env,
		Cwd:     options.Cwd,
		Timeout: options.Timeout,
		User:    options.User,
		TTY:     options.TTY,
	})
	if err != nil {
		return ExecResult{}, mapRPCError(err, "sandbox exec", n.allocationID)
	}
	sdkResult := ExecResult{
		ExitCode:        result.ExitCode,
		Stdout:          result.Stdout,
		Stderr:          result.Stderr,
		StdoutTruncated: result.StdoutTruncated,
		StderrTruncated: result.StderrTruncated,
	}
	if options.Check && sdkResult.ExitCode != 0 {
		return sdkResult, &ExecError{Argv: argv, Result: sdkResult}
	}
	return sdkResult, nil
}

func (n *AllocationClient) rpcClient() *nodeclient.Client {
	return nodeclient.New(
		n.allocationID,
		n.client.nodes,
	)
}

func (n *AllocationClient) validate() error {
	if n == nil || n.client == nil {
		return requiredError("client")
	}
	if n.allocationID == "" {
		return requiredError("allocation_id")
	}
	return nil
}

func errPathRequired() error {
	return &PathError{Message: "path is required"}
}

func errSrcDstPathRequired() error {
	return &PathError{Message: "src_path and dst_path are required"}
}

// PathError describes invalid sandbox path input.
type PathError struct {
	Message string
}

func (e *PathError) Error() string {
	return e.Message
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}

func validateExecOptions(options ExecOptions) error {
	if options.Timeout < 0 {
		return positiveDurationError("timeout")
	}
	return nil
}
