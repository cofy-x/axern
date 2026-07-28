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
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	ManagedProxy *ManagedProxyOptions
}

type ManagedProxyOptions struct {
	Provider            string
	UpstreamBaseURL     string
	UpstreamBearerToken string
}

type ImageProcessMount struct {
	SandboxPath string
	TargetPath  string
	Readonly    bool
	Options     []string
}

type ImageOptions struct {
	Env          map[string]string
	Cwd          string
	Timeout      time.Duration
	User         string
	TTY          bool
	Mounts       []ImageProcessMount
	ManagedProxy *ManagedProxyOptions
}

type Result struct {
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
			Tty:            options.TTY,
			ManagedProxy:   managedProxySpec(options.ManagedProxy),
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExitCode:           response.GetExitCode(),
		Stdout:             bytes.Clone(response.GetStdout()),
		Stderr:             bytes.Clone(response.GetStderr()),
		StdoutTruncated:    response.GetStdoutTruncated(),
		StderrTruncated:    response.GetStderrTruncated(),
		ManagedProxyReport: managedProxyReport(response.GetManagedProxyReport()),
	}, nil
}

func (c *Client) ExecImage(ctx context.Context, image string, argv []string, options ImageOptions) (Result, error) {
	response, err := c.nodes.ExecImage(ctx, &nodesandboxv1.ExecImageRequest{
		AllocationID: c.allocationID,
		Spec:         imageProcessSpec(image, argv, options),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		ExitCode:           response.GetExitCode(),
		Stdout:             bytes.Clone(response.GetStdout()),
		Stderr:             bytes.Clone(response.GetStderr()),
		StdoutTruncated:    response.GetStdoutTruncated(),
		StderrTruncated:    response.GetStderrTruncated(),
		ManagedProxyReport: managedProxyReport(response.GetManagedProxyReport()),
	}, nil
}

func imageProcessSpec(image string, argv []string, options ImageOptions) *nodesandboxv1.ImageProcessSpec {
	return &nodesandboxv1.ImageProcessSpec{
		Image:          image,
		Argv:           append([]string(nil), argv...),
		Env:            cloneMap(options.Env),
		Cwd:            options.Cwd,
		TimeoutSeconds: int64(options.Timeout.Seconds()),
		User:           options.User,
		Tty:            options.TTY,
		Mounts:         imageProcessMounts(options.Mounts),
		ManagedProxy:   managedProxySpec(options.ManagedProxy),
	}
}

func managedProxySpec(options *ManagedProxyOptions) *nodesandboxv1.ManagedProxySpec {
	if options == nil {
		return nil
	}
	return &nodesandboxv1.ManagedProxySpec{
		Provider:            options.Provider,
		UpstreamBaseUrl:     options.UpstreamBaseURL,
		UpstreamBearerToken: options.UpstreamBearerToken,
	}
}

func managedProxyReport(report *nodesandboxv1.ManagedProxyReport) *ManagedProxyReport {
	if report == nil {
		return nil
	}
	return &ManagedProxyReport{
		Provider:      report.GetProvider(),
		RequestCount:  report.GetRequestCount(),
		ResponseCount: report.GetResponseCount(),
		ErrorCount:    report.GetErrorCount(),
		ReportJSON:    bytes.Clone(report.GetReportJson()),
	}
}

func imageProcessMounts(mounts []ImageProcessMount) []*nodesandboxv1.ImageProcessMount {
	if mounts == nil {
		mounts = []ImageProcessMount{{SandboxPath: "/workspace", TargetPath: "/workspace"}}
	}
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*nodesandboxv1.ImageProcessMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &nodesandboxv1.ImageProcessMount{
			SandboxPath: mount.SandboxPath,
			TargetPath:  mount.TargetPath,
			Readonly:    mount.Readonly,
			Options:     append([]string(nil), mount.Options...),
		})
	}
	return out
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
