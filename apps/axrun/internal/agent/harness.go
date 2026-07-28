package agent

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type Harness interface {
	Preflight() error
	Run(context.Context, Request) (Result, error)
}

type Request struct {
	Agent        domain.AgentSpec
	Model        domain.ModelSpec
	Task         domain.TaskInstance
	Episode      domain.Episode
	Sandbox      sandbox.Instance
	Instruction  string
	ArtifactDir  string
	ManagedProxy *sandbox.ManagedProxyOptions
	Recorder     *proxy.Recorder
}

// ManagedProxyConfigurer is implemented by harnesses that require sandboxd to
// run a provider-backed recording proxy alongside the agent process.
type ManagedProxyConfigurer interface {
	ManagedProxyConfig(agentSpec domain.AgentSpec) (*ManagedProxyConfig, error)
}

type ProviderCapabilityProber interface {
	ProbeProvider(context.Context, domain.AgentSpec, domain.ModelSpec) (agentprofile.ProbeResult, error)
}

type ManagedProxyConfig struct {
	Upstream     *url.URL
	Token        string
	ProviderType ProviderType
}

type Result = domain.AgentResult

type NoopHarness struct{}

func (NoopHarness) Preflight() error {
	return nil
}

func (NoopHarness) Run(context.Context, Request) (Result, error) {
	return Result{Status: domain.AgentStatusCompleted, Summary: "noop agent completed"}, nil
}

type OracleShellHarness struct {
	Command    string
	CWD        string
	TimeoutSec int
}

func (h OracleShellHarness) Preflight() error {
	if h.Command == "" {
		return fmt.Errorf("oracle shell agent command is required")
	}
	return nil
}

func (h OracleShellHarness) Run(ctx context.Context, request Request) (Result, error) {
	execResult, err := request.Sandbox.Exec(
		ctx,
		sandbox.ShellCommand(h.Command),
		sandbox.ExecOptions{
			CWD:     h.CWD,
			Timeout: h.timeout(),
		},
	)
	if err != nil {
		return Result{}, err
	}
	exitCode := execResult.ExitCode
	if execResult.ExitCode != 0 {
		return Result{
			Status:   domain.AgentStatusFailed,
			Summary:  fmt.Sprintf("oracle shell agent exited with status %d", execResult.ExitCode),
			Error:    fmt.Sprintf("command exited with status %d", execResult.ExitCode),
			ExitCode: &exitCode,
			Stdout:   execResult.Stdout,
			Stderr:   execResult.Stderr,
		}, nil
	}
	return Result{
		Status:   domain.AgentStatusCompleted,
		Summary:  "oracle shell agent completed",
		ExitCode: &exitCode,
		Stdout:   execResult.Stdout,
		Stderr:   execResult.Stderr,
	}, nil
}

func (h OracleShellHarness) timeout() time.Duration {
	if h.TimeoutSec <= 0 {
		return 30 * time.Second
	}
	return time.Duration(h.TimeoutSec) * time.Second
}
