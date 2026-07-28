package axernsdk

import (
	"context"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/nodeclient"
)

// ExecOptions configures a collected sandbox command execution.
type ExecOptions struct {
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	Check        bool
	ManagedProxy *ManagedProxyOptions
}

type ManagedProxyOptions struct {
	Provider            string
	UpstreamBaseURL     string
	UpstreamBearerToken string
}

// ExecResult contains collected command output and exit status.
type ExecResult struct {
	ExitCode           int32
	Stdout             []byte
	Stderr             []byte
	StdoutTruncated    bool
	StderrTruncated    bool
	ManagedProxyReport *ManagedProxyReport
}

type ManagedProxyReport struct {
	Provider      string
	RequestCount  int32
	ResponseCount int32
	ErrorCount    int32
	ReportJSON    []byte
}

// StdoutString returns stdout decoded as a Go string.
func (r ExecResult) StdoutString() string {
	return string(r.Stdout)
}

// StderrString returns stderr decoded as a Go string.
func (r ExecResult) StderrString() string {
	return string(r.Stderr)
}

// NodeSandboxClient provides lower-level operations for an existing allocation.
type NodeSandboxClient struct {
	client       *Client
	allocationID string
}

// NodeSandbox returns a low-level sandbox client for allocationID.
func (c *Client) NodeSandbox(allocationID string) (*NodeSandboxClient, error) {
	if c == nil {
		return nil, requiredError("client")
	}
	if allocationID == "" {
		return nil, requiredError("allocation_id")
	}
	return &NodeSandboxClient{client: c, allocationID: allocationID}, nil
}

// Exec runs a command in the allocation and collects stdout/stderr.
func (n *NodeSandboxClient) Exec(ctx context.Context, command any, options ExecOptions) (ExecResult, error) {
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
		Env:          options.Env,
		Cwd:          options.Cwd,
		Timeout:      options.Timeout,
		User:         options.User,
		TTY:          options.TTY,
		ManagedProxy: nodeManagedProxyOptions(options.ManagedProxy),
	})
	if err != nil {
		return ExecResult{}, mapRPCError(err, "sandbox exec", n.allocationID)
	}
	sdkResult := ExecResult{
		ExitCode:           result.ExitCode,
		Stdout:             result.Stdout,
		Stderr:             result.Stderr,
		StdoutTruncated:    result.StdoutTruncated,
		StderrTruncated:    result.StderrTruncated,
		ManagedProxyReport: sdkManagedProxyReport(result.ManagedProxyReport),
	}
	if options.Check && sdkResult.ExitCode != 0 {
		return sdkResult, &ExecError{Argv: argv, Result: sdkResult}
	}
	return sdkResult, nil
}

func nodeManagedProxyOptions(options *ManagedProxyOptions) *nodeclient.ManagedProxyOptions {
	if options == nil {
		return nil
	}
	return &nodeclient.ManagedProxyOptions{
		Provider:            options.Provider,
		UpstreamBaseURL:     options.UpstreamBaseURL,
		UpstreamBearerToken: options.UpstreamBearerToken,
	}
}

func sdkManagedProxyReport(report *nodeclient.ManagedProxyReport) *ManagedProxyReport {
	if report == nil {
		return nil
	}
	return &ManagedProxyReport{
		Provider:      report.Provider,
		RequestCount:  report.RequestCount,
		ResponseCount: report.ResponseCount,
		ErrorCount:    report.ErrorCount,
		ReportJSON:    append([]byte(nil), report.ReportJSON...),
	}
}

func (n *NodeSandboxClient) rpcClient() *nodeclient.Client {
	return nodeclient.New(
		n.allocationID,
		n.client.nodes,
	)
}

func (n *NodeSandboxClient) validate() error {
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
