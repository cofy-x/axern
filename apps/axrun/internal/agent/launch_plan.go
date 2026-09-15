package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agentimage"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type LaunchPlan struct {
	LauncherKind     domain.AgentLauncherKind
	RuntimeType      domain.AgentRuntimeType
	Image            string
	ImageMountTarget string
	Command          sandbox.ExecCommand
	CWD              string
	User             string
	Timeout          time.Duration
	Env              map[string]string
	Profile          string
	SessionMode      domain.AgentSessionMode
	SessionID        string
	MaxTurns         int
	OutputFormat     string
	AllowedTools     []string
	IdleTimeoutSec   int
}

func (p LaunchPlan) Validate() error {
	switch p.LauncherKind {
	case domain.AgentLauncherKindSandboxCommand:
		if err := p.Command.Validate(); err != nil {
			return fmt.Errorf("agent launch command: %w", err)
		}
	case domain.AgentLauncherKindAgentImage:
		if p.Image == "" {
			return fmt.Errorf("agent-image launcher requires an image")
		}
		if !agentimage.ValidMountTarget(p.ImageMountTarget) {
			return fmt.Errorf("agent-image launcher requires an image mount target")
		}
		if err := p.Command.Validate(); err != nil {
			return fmt.Errorf("agent launch command: %w", err)
		}
	default:
		return fmt.Errorf("unsupported agent launcher kind %q", p.LauncherKind)
	}
	return nil
}

type Launcher interface {
	Kind() domain.AgentLauncherKind
	Launch(context.Context, sandbox.Instance, LaunchPlan) (sandbox.ExecResult, error)
}

// LauncherSetter is implemented by harnesses whose launcher is provided by the
// backend composition root.
type LauncherSetter interface {
	SetLauncher(Launcher)
}

type SandboxCommandLauncher struct{}

func (SandboxCommandLauncher) Kind() domain.AgentLauncherKind {
	return domain.AgentLauncherKindSandboxCommand
}

func (SandboxCommandLauncher) Launch(ctx context.Context, instance sandbox.Instance, plan LaunchPlan) (sandbox.ExecResult, error) {
	if instance == nil {
		return sandbox.ExecResult{}, fmt.Errorf("agent launcher sandbox is required")
	}
	if err := plan.Validate(); err != nil {
		return sandbox.ExecResult{}, err
	}
	return instance.Exec(ctx, plan.Command, plan.ExecOptions())
}

func (p LaunchPlan) ExecOptions() sandbox.ExecOptions {
	return sandbox.ExecOptions{
		CWD:     p.CWD,
		User:    p.User,
		Timeout: p.Timeout,
		Env:     cloneEnv(p.Env),
	}
}

type MountedAgentImageLauncher struct{}

func (MountedAgentImageLauncher) Kind() domain.AgentLauncherKind {
	return domain.AgentLauncherKindAgentImage
}

func (l MountedAgentImageLauncher) Launch(ctx context.Context, instance sandbox.Instance, plan LaunchPlan) (sandbox.ExecResult, error) {
	if instance == nil {
		return sandbox.ExecResult{}, fmt.Errorf("agent-image launcher sandbox is required")
	}
	if err := plan.Validate(); err != nil {
		return sandbox.ExecResult{}, err
	}
	if err := l.selfCheck(ctx, instance, plan); err != nil {
		return sandbox.ExecResult{}, err
	}
	command := commandWithAgentImagePath(plan.Command, AgentImageBinDir(plan.ImageMountTarget))
	return instance.Exec(ctx, command, plan.execOptionsWithAgentImageMount())
}

func (l MountedAgentImageLauncher) selfCheck(ctx context.Context, instance sandbox.Instance, plan LaunchPlan) error {
	target := shellQuote(plan.ImageMountTarget)
	binDir := shellQuote(AgentImageBinDir(plan.ImageMountTarget))
	result, err := instance.Exec(ctx, sandbox.ShellCommand("test -d "+target+" && test -d "+binDir), plan.ExecOptions())
	if err != nil {
		return fmt.Errorf("agent image self-check failed: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("agent image self-check exited with status %d: expected %s and %s to exist", result.ExitCode, plan.ImageMountTarget, AgentImageBinDir(plan.ImageMountTarget))
	}
	return nil
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(env))
	for key, value := range env {
		cloned[key] = value
	}
	return cloned
}

func AgentImageMountTarget(agentName string) string {
	return agentimage.MountTarget(agentName)
}

func AgentImageMountTargetForSpec(spec domain.AgentSpec) string {
	return agentimage.MountTargetForSpec(spec)
}

func AgentImageBinDir(mountTarget string) string {
	return agentimage.BinDir(mountTarget)
}

func (p LaunchPlan) execOptionsWithAgentImageMount() sandbox.ExecOptions {
	options := p.ExecOptions()
	env := cloneEnv(options.Env)
	if env == nil {
		env = map[string]string{}
	}
	if strings.TrimSpace(env["AXRUN_AGENT_IMAGE_MOUNT_TARGET"]) == "" {
		env["AXRUN_AGENT_IMAGE_MOUNT_TARGET"] = p.ImageMountTarget
	}
	options.Env = env
	return options
}

func commandWithAgentImagePath(command sandbox.ExecCommand, binDir string) sandbox.ExecCommand {
	prelude := "export PATH=" + shellQuote(binDir) + `:"${PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}"`
	return WrapCommandWithShellPrelude(prelude, command)
}
