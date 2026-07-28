package axern

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/agentbundle"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/runtimeimage"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
	sandboxaxern "github.com/cofy-x/axern/apps/axrun/internal/sandbox/axern"
	axernsdk "github.com/cofy-x/axern/sdk/go"
)

type Adapter struct {
	Config    Config
	AgentName string
	Now       func() time.Time
	Runtime   sandbox.Runtime
	Agent     agent.Harness
	Registry  *agent.Registry
	Images    runtimeimage.Resolver
}

var _ backend.AgentPreflight = Adapter{}
var _ backend.ProviderPreflight = Adapter{}
var _ backend.ProviderProfilePreflight = Adapter{}
var _ backend.TaskPreflight = Adapter{}

type Option func(*Adapter)

func WithNow(now func() time.Time) Option {
	return func(a *Adapter) {
		a.Now = now
	}
}

func WithAgentName(name string) Option {
	return func(a *Adapter) {
		a.AgentName = name
	}
}

func WithRuntime(runtime sandbox.Runtime) Option {
	return func(a *Adapter) {
		a.Runtime = runtime
	}
}

func WithAgent(harness agent.Harness) Option {
	return func(a *Adapter) {
		a.Agent = harness
	}
}

func WithRegistry(registry *agent.Registry) Option {
	return func(a *Adapter) {
		a.Registry = registry
	}
}

func WithImageResolver(resolver runtimeimage.Resolver) Option {
	return func(a *Adapter) {
		a.Images = resolver
	}
}

func NewFromEnv(options ...Option) Adapter {
	config := ConfigFromEnv()
	return New(config, options...)
}

func New(config Config, options ...Option) Adapter {
	adapter := Adapter{Config: config}
	for _, option := range options {
		option(&adapter)
	}
	return adapter
}

func (a Adapter) Preflight() error {
	if err := a.runtime().Preflight(); err != nil {
		return err
	}
	if a.Agent != nil {
		return a.Agent.Preflight()
	}
	if a.Registry != nil && a.Registry.IsKnown(a.AgentName) {
		h, err := a.Registry.NewHarness(a.AgentName, domain.AgentSpec{Name: a.AgentName})
		if err != nil {
			return err
		}
		if h != nil {
			return h.Preflight()
		}
	}
	return nil
}

func (a Adapter) PreflightTasks(tasks []domain.TaskInstance) error {
	for _, task := range tasks {
		source := task.Sandbox.RuntimeSource
		if source == nil {
			if a.Config.TemplateID != "" || a.Config.Image != "" {
				continue
			}
			return fmt.Errorf("axern backend requires AXERN_TEMPLATE_ID, AXERN_IMAGE, or task sandbox runtime_source")
		}
		if source.Type != domain.SandboxRuntimeSourceDockerfile {
			continue
		}
		if a.Images != nil {
			continue
		}
		resolver := runtimeimage.NewDockerResolverFromEnv()
		if err := runtimeimage.ValidateResolverConfig(resolver); err != nil {
			return err
		}
	}
	return nil
}

func (a Adapter) PreflightAgent(agent domain.AgentSpec) error {
	return backend.ValidateAgentRuntimeSupport(string(backend.NameAxern), agent)
}

func (a Adapter) PreflightProvider(ctx context.Context, agentSpec domain.AgentSpec, model domain.ModelSpec) error {
	harness, err := a.agentHarness(agentSpec)
	if err != nil {
		return err
	}
	return backend.PreflightHarnessProvider(ctx, harness, agentSpec, model)
}

func (a Adapter) PreflightProviderProfile(agentSpec domain.AgentSpec) error {
	harness, err := a.agentHarness(agentSpec)
	if err != nil {
		return err
	}
	return backend.PreflightHarnessProfile(harness, agentSpec)
}

func (a Adapter) Execute(request backend.ExecuteRequest) (episode domain.Episode, runErr error) {
	if err := a.PreflightAgent(request.Episode.Agent); err != nil {
		return request.Episode, err
	}
	if a.Registry != nil {
		if err := a.Registry.ValidateAgent(a.AgentName, request.Episode.Agent); err != nil {
			return request.Episode, err
		}
	}
	agentHarness, err := a.agentHarness(request.Episode.Agent)
	if err != nil {
		return request.Episode, err
	}
	resolvedRequest, err := a.resolveRuntimeSource(request)
	if err != nil {
		return request.Episode, err
	}
	runtime, err := a.runtimeForRequest(resolvedRequest)
	if err != nil {
		return request.Episode, err
	}
	return rollout.Execute(rollout.Request{
		Store:          resolvedRequest.Store,
		Task:           resolvedRequest.Task,
		Episode:        resolvedRequest.Episode,
		Paths:          resolvedRequest.Paths,
		SandboxRuntime: runtime,
		AgentHarness:   agentHarness,
		Now:            a.Now,
		RuntimeName:    "axern",
		HealthCheck:    rollout.HealthCheckConfigFromEnv(),
		PhaseReporter:  request.PhaseReporter,
	})
}

func (a Adapter) runtime() sandbox.Runtime {
	if a.Runtime != nil {
		return a.Runtime
	}
	return sandboxaxern.NewRuntime(a.Config)
}

func (a Adapter) runtimeForRequest(request backend.ExecuteRequest) (sandbox.Runtime, error) {
	if a.Runtime != nil {
		return a.Runtime, nil
	}
	config, err := a.configForTask(request.Task)
	if err != nil {
		return nil, err
	}
	if request.Episode.Agent.Runtime != nil && request.Episode.Agent.Runtime.Type == domain.AgentRuntimeTypeAgentImage {
		config.ImageMounts = append(config.ImageMounts, agentBundleImageMount(request.Episode.Agent))
	}
	return sandboxaxern.NewRuntime(config), nil
}

func (a Adapter) configForTask(task domain.TaskInstance) (Config, error) {
	config := a.Config
	if resources := task.Resources; resources != nil {
		if strings.TrimSpace(resources.Disk) != "" {
			return Config{}, fmt.Errorf("task resources disk is not supported by the Axern sandbox API")
		}
		config.RequestCPU = strings.TrimSpace(resources.RequestCPU)
		config.RequestMemory = strings.TrimSpace(resources.RequestMemory)
		config.LimitCPU = strings.TrimSpace(resources.LimitCPU)
		config.LimitMemory = strings.TrimSpace(resources.LimitMemory)
	}
	if task.InitialState != nil && task.InitialState.WorkspaceImage != nil {
		workspace := task.InitialState.WorkspaceImage
		config.WorkspaceImage = &axernsdk.WorkspaceImageSource{SourcePath: workspace.SourcePath, Target: workspace.Target}
		for _, variant := range workspace.Variants {
			config.WorkspaceImage.Variants = append(
				config.WorkspaceImage.Variants,
				axernsdk.WorkspaceImageVariant{
					Format: variant.Format,
					Image:  variant.Image,
				},
			)
		}
	}
	if runtimeClass := strings.TrimSpace(task.Sandbox.RuntimeClass); runtimeClass != "" {
		config.RuntimeClass = runtimeClass
	}
	if task.Sandbox.RuntimeSource != nil {
		switch task.Sandbox.RuntimeSource.Type {
		case domain.SandboxRuntimeSourceTemplate:
			config.Image = ""
			config.TemplateID = task.Sandbox.RuntimeSource.TemplateID
			return config, nil
		case domain.SandboxRuntimeSourceImage:
			config.TemplateID = ""
			config.Image = task.Sandbox.RuntimeSource.Image
			return config, nil
		case domain.SandboxRuntimeSourceDockerfile:
			return Config{}, fmt.Errorf("unresolved dockerfile runtime source %q", task.Sandbox.RuntimeSource.Dockerfile)
		}
	}
	if config.TemplateID != "" || config.Image != "" {
		return config, nil
	}
	return Config{}, fmt.Errorf("axern backend requires AXERN_TEMPLATE_ID, AXERN_IMAGE, or task sandbox runtime_source")
}

func (a Adapter) agentHarness(spec domain.AgentSpec) (agent.Harness, error) {
	if a.Agent != nil {
		return a.Agent, nil
	}
	if a.Registry == nil || !a.Registry.IsKnown(spec.Name) {
		return nil, nil
	}
	h, err := a.Registry.NewHarness(spec.Name, spec)
	if err != nil || h == nil {
		return h, err
	}
	if spec.Runtime != nil && spec.Runtime.Type == domain.AgentRuntimeTypeAgentImage {
		if setter, ok := h.(agent.LauncherSetter); ok {
			setter.SetLauncher(agent.MountedBundleLauncher{})
		} else {
			return nil, fmt.Errorf("agent %q runtime agent-image requires backend launcher support", spec.Name)
		}
	}
	return h, nil
}

func agentBundleImageMount(spec domain.AgentSpec) axernsdk.ImageMount {
	image := ""
	if spec.Runtime != nil {
		image = spec.Runtime.Image
	}
	return axernsdk.ImageMount{
		Image:    image,
		Target:   agentbundle.MountTargetForSpec(spec),
		Readonly: true,
	}
}
