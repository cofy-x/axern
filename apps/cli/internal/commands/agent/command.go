package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	appagent "github.com/cofy-x/axern/apps/cli/internal/application/agent"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	cliconfig "github.com/cofy-x/axern/apps/cli/internal/config"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/tunnelrelay"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{
		Use:   "agent",
		Short: "Run interactive agent development workflows",
		Args:  command.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return command.Usage(fmt.Errorf("agent requires shell, run, connect, doctor, list, stop, workspace, or profile"))
		},
	}
	root.AddCommand(workflowCommand(runtime, "shell", appagent.ModeShell), workflowCommand(runtime, "run", appagent.ModeRun), workflowCommand(runtime, "connect", appagent.ModeConnect), doctorCommand(runtime), runtimeListCommand(runtime), runtimeStopCommand(runtime), workspaceCommand(runtime), profileCommand(runtime))
	return root
}

func workspaceCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "workspace", Short: "Manage agent workspaces", Args: command.NoArgs}
	root.AddCommand(workspaceDeleteCommand(runtime))
	return root
}

func workspaceDeleteCommand(runtime command.Runtime) *cobra.Command {
	var workspace string
	var yes bool
	var timeout time.Duration
	cmd := &cobra.Command{Use: "delete", Short: "Permanently delete a suspended agent workspace", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if strings.TrimSpace(workspace) == "" {
			return command.Usage(fmt.Errorf("--workspace is required"))
		}
		if timeout < 0 {
			return command.Usage(fmt.Errorf("--timeout must be non-negative"))
		}
		resolved, err := appagent.ResolveWorkspaceName(workspace, workspace)
		if err != nil {
			return command.Usage(err)
		}
		if !yes {
			input, ok := cmd.InOrStdin().(*os.File)
			if !ok {
				return command.Usage(fmt.Errorf("--yes is required for non-interactive deletion"))
			}
			info, statErr := input.Stat()
			if statErr != nil || info.Mode()&os.ModeCharDevice == 0 {
				return command.Usage(fmt.Errorf("--yes is required for non-interactive deletion"))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Type %s to permanently delete this workspace: ", resolved)
			line, readErr := bufio.NewReader(input).ReadString('\n')
			if readErr != nil || strings.TrimSpace(line) != resolved {
				return command.Usage(fmt.Errorf("workspace deletion confirmation did not match %q", resolved))
			}
		}
		format, err := runtime.Format()
		if err != nil {
			return err
		}
		session, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		result, err := appagent.New().DeleteWorkspace(session.Context, session.Clients.Service, resolved, timeout)
		if err != nil {
			return err
		}
		return renderDeleteResult(cmd.OutOrStdout(), result, format)
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "agent workspace to permanently delete")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm permanent deletion without an interactive prompt")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "physical deletion wait timeout; 0 waits indefinitely")
	return cmd
}

func runtimeListCommand(runtime command.Runtime) *cobra.Command {
	var profile, workspace string
	cmd := &cobra.Command{Use: "list", Short: "List active agent runtimes", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		format, err := runtime.Format()
		if err != nil {
			return err
		}
		session, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		runtimes, err := appagent.New().ListRuntimes(session.Context, session.Clients.Service, workspace, profile)
		if err != nil {
			return err
		}
		return renderRuntimeList(cmd.OutOrStdout(), runtimes, format)
	}}
	cmd.Flags().StringVar(&profile, "profile", "", "filter by agent profile")
	cmd.Flags().StringVar(&workspace, "workspace", "", "filter by agent workspace")
	return cmd
}

func runtimeStopCommand(runtime command.Runtime) *cobra.Command {
	var profile, workspace string
	cmd := &cobra.Command{Use: "stop", Short: "Suspend an agent workspace and retain its data", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		format, err := runtime.Format()
		if err != nil {
			return err
		}
		if strings.TrimSpace(workspace) == "" {
			resolvedProfile, err := loadProfileDefaults(runtime, profile)
			if err != nil {
				return command.Usage(err)
			}
			workspace, err = appagent.ResolveWorkspaceName(workspace, resolvedProfile.Name)
			if err != nil {
				return command.Usage(err)
			}
		} else {
			workspace, err = appagent.ResolveWorkspaceName(workspace, workspace)
			if err != nil {
				return command.Usage(err)
			}
		}
		session, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		result, err := appagent.New().Stop(session.Context, session.Clients.Service, workspace)
		if err != nil {
			return err
		}
		return renderStopResult(cmd.OutOrStdout(), result, format)
	}}
	cmd.Flags().StringVar(&profile, "profile", "", "profile used to derive the default workspace when --workspace is omitted")
	cmd.Flags().StringVar(&workspace, "workspace", "", "agent workspace; defaults to the selected profile name")
	return cmd
}

type workflowOptions struct {
	profile, workspace, sshTarget, sshIdentity                   string
	ttl, readyTimeout, serviceTimeout, pingInterval, pongTimeout time.Duration
	maxStreams                                                   int
	strict                                                       bool
}

func workflowCommand(runtime command.Runtime, name string, mode appagent.Mode) *cobra.Command {
	o := &workflowOptions{ttl: 30 * time.Minute, readyTimeout: 30 * time.Second, serviceTimeout: 2 * time.Minute, pingInterval: 15 * time.Second, pongTimeout: 45 * time.Second, maxStreams: 256}
	cmd := &cobra.Command{Use: name, Short: map[string]string{"shell": "Open an agent shell", "run": "Run the agent CLI", "connect": "Start the agent adapter tunnel"}[name], Args: func(cmd *cobra.Command, args []string) error {
		if mode != appagent.ModeRun && len(args) != 0 {
			return command.NoArgs(cmd, args)
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error { return start(runtime, cmd, mode, args, *o) }}
	f := cmd.Flags()
	f.StringVar(&o.profile, "profile", "", "agent profile")
	f.StringVar(&o.workspace, "workspace", "", "agent workspace; defaults to the selected profile name")
	f.DurationVar(&o.ttl, "ttl", o.ttl, "tunnel TTL")
	f.DurationVar(&o.readyTimeout, "ready-timeout", o.readyTimeout, "tunnel readiness timeout")
	f.DurationVar(&o.serviceTimeout, "service-timeout", o.serviceTimeout, "service readiness timeout")
	f.DurationVar(&o.pingInterval, "ping-interval", o.pingInterval, "connector ping interval")
	f.DurationVar(&o.pongTimeout, "pong-timeout", o.pongTimeout, "connector pong timeout")
	f.IntVar(&o.maxStreams, "max-streams", o.maxStreams, "maximum streams")
	f.StringVar(&o.sshTarget, "ssh-endpoint", "", "gateway SSH endpoint")
	f.StringVar(&o.sshIdentity, "ssh-identity-file", "", "gateway SSH identity")
	f.BoolVar(&o.strict, "strict-host-key-checking", false, "use known_hosts verification")
	return cmd
}

func start(runtime command.Runtime, cmd *cobra.Command, mode appagent.Mode, args []string, options workflowOptions) error {
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	profile, err := loadProfile(runtime, options.profile)
	if err != nil {
		return command.Usage(err)
	}
	_, contextProfile, _, err := runtime.ResolveContext()
	if err != nil {
		return command.Usage(err)
	}
	remote, err := resolveRemoteTarget(contextProfile, profile.RemoteUser, options.sshTarget, options.sshIdentity, options.strict)
	if err != nil {
		return command.Usage(err)
	}
	connection, err := runtime.ResolveConnection()
	if err != nil {
		return command.Usage(err)
	}
	session, err := connection.Open(cmd.Context())
	if err != nil {
		return err
	}
	defer session.Close()
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return appagent.New().Start(signalCtx, appagent.Params{CreateContext: session.Context, Profile: profile, Workspace: options.workspace, ServiceClient: session.Clients.Service, Catalog: session.Clients.Catalog, Environment: session.Clients.Environment, Tunnel: apptunnel.New(session.Clients.Tunnel), Remote: sshRemoteRunner{}, RemoteTarget: remote, Mode: mode, RunArgs: args, TTL: options.ttl, ReadyTimeout: options.readyTimeout, ServiceTimeout: options.serviceTimeout, Relay: tunnelrelay.Config(connection.Config), RelayDialer: tunnelrelay.PeerDialer, Connector: apptunnel.ConnectorConfig{PingInterval: options.pingInterval, PongTimeout: options.pongTimeout, MaxStreams: options.maxStreams}, OnReconnect: func(err error, backoff time.Duration) {
		fmt.Fprintf(cmd.ErrOrStderr(), "agent tunnel disconnected: %v; reconnecting in %s\n", err, backoff)
	}, OnReady: func(result appagent.Result) error {
		if err := renderReady(cmd.OutOrStdout(), result, format); err != nil {
			return err
		}
		if mode == appagent.ModeConnect && format != output.FormatJSON {
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl-C to stop.")
		}
		return nil
	}})
}

func doctorCommand(runtime command.Runtime) *cobra.Command {
	var profileName, workspace, model string
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose an agent profile", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		format, err := runtime.Format()
		if err != nil {
			return err
		}
		profile, err := loadProfile(runtime, profileName)
		if err != nil {
			if renderErr := renderDoctor(cmd, appagent.FailedDoctorResult(effectiveProfileName(profileName), workspace, err), format); renderErr != nil {
				return renderErr
			}
			return command.ExitError{Code: 2, Err: fmt.Errorf("agent profile is invalid")}
		}
		session, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		result, err := appagent.New().Doctor(session.Context, appagent.DoctorParams{Profile: profile, Workspace: workspace, ProbeModel: model, ServiceClient: session.Clients.Service, Catalog: session.Clients.Catalog, Environment: session.Clients.Environment})
		if err != nil {
			return err
		}
		if err := renderDoctor(cmd, result, format); err != nil {
			return err
		}
		if (result.UpstreamCheck != nil && !result.UpstreamCheck.Compatible) || (result.PlatformCheck != nil && !result.PlatformCheck.Reachable) {
			return command.ExitError{Code: 1, Err: fmt.Errorf("agent doctor found an unhealthy dependency")}
		}
		return nil
	}}
	cmd.Flags().StringVar(&profileName, "profile", "", "agent profile")
	cmd.Flags().StringVar(&workspace, "workspace", "", "agent workspace; defaults to the selected profile name")
	cmd.Flags().StringVar(&model, "model", "", "model used for the provider capability probe")
	return cmd
}

func renderDoctor(cmd *cobra.Command, result appagent.DoctorResult, format output.Format) error {
	if format == output.FormatJSON {
		return output.PrintJSON(cmd.OutOrStdout(), result)
	}
	if result.Agent != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Agent: %s\nProvider: %s\nWorkspace template: %s\nAgent bundle: %s\nWorkspace: %s\n", result.Agent, result.Provider, result.WorkspaceTemplate, result.AgentBundle, result.Workspace)
	}
	if result.UpstreamCheck != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Required API: %s\nUpstream compatible: %t\nUpstream status: %s\n", result.UpstreamCheck.WireAPI, result.UpstreamCheck.Compatible, result.UpstreamCheck.Message)
	}
	if result.PlatformCheck != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Platform reachable: %t\nPlatform status: %s\n", result.PlatformCheck.Reachable, result.PlatformCheck.Message)
	}
	if result.LifecycleState != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Workspace state: %s\nPersistent: %t\n", result.LifecycleState, result.Persistent)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Agent profile: %s\nConfig OK: %t\nApproval compatible: %t\nAxern policy: %s\nLocal policy: %s\nRecommendation: %s\n", result.Profile, result.ConfigOK, result.ApprovalCompatible, result.AxernApprovalPolicy, result.LocalApprovalPolicy, result.Recommendation)
	return nil
}

func loadProfile(runtime command.Runtime, name string) (agentprofile.Profile, error) {
	profile, err := loadProfileDefaults(runtime, name)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	if err := appagent.ValidateProfile(profile); err != nil {
		return agentprofile.Profile{}, err
	}
	return profile, nil
}

func loadProfileDefaults(runtime command.Runtime, name string) (agentprofile.Profile, error) {
	resolved, profile, ok, err := cliconfig.ResolveAgentProfile(runtime.Options.ConfigPath, name)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	if !ok {
		return agentprofile.Profile{}, fmt.Errorf("agent profile %q not found", resolved)
	}
	applyProfileDefaults(&profile)
	return profile, nil
}

func effectiveProfileName(name string) string {
	if strings.TrimSpace(name) == "" {
		return appagent.DefaultProfileName
	}
	return strings.TrimSpace(name)
}

func applyProfileDefaults(profile *agentprofile.Profile) {
	adapter, err := appagent.AdapterFor(profile.Agent)
	if err == nil && profile.TemplateID == "" {
		profile.TemplateID = adapter.DefaultTemplateID()
	}
	if profile.Namespace == "" {
		profile.Namespace = appagent.DefaultNamespace
	}
	if profile.RemoteUser == "" {
		profile.RemoteUser = appagent.DefaultRemoteUser
	}
}

func profileCommand(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "profile", Short: "Manage agent profiles"}
	root.AddCommand(profileSetCommand(runtime), profileGetCommand(runtime), profileListCommand(runtime), profileDeleteCommand(runtime), profileUseCommand(runtime))
	return root
}

func profileSetCommand(runtime command.Runtime) *cobra.Command {
	var agentType, provider, upstream, token, template, namespace, remoteUser, model string
	var configValues, envValues []string
	var restore, use bool
	cmd := &cobra.Command{Use: "set <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		parsedAgent, err := agentprofile.ParseAgentType(agentType)
		if err != nil {
			return command.Usage(err)
		}
		parsedProvider, err := agentprofile.ParseProviderType(provider)
		if err != nil {
			return command.Usage(err)
		}
		wireAPI, err := agentprofile.RequiredWireAPI(parsedAgent)
		if err != nil {
			return command.Usage(err)
		}
		if _, err := agentprofile.ParseUpstream("agent profile", upstream); err != nil {
			return command.Usage(err)
		}
		if token == "" {
			return command.Usage(fmt.Errorf("token is required"))
		}
		configMap, err := keyValues(configValues)
		if err != nil {
			return command.Usage(err)
		}
		envMap, err := keyValues(envValues)
		if err != nil {
			return command.Usage(err)
		}
		if model != "" {
			if configMap == nil {
				configMap = map[string]string{}
			}
			configMap[defaultModelConfigKey(parsedAgent)] = model
		}
		stored := &agentprofile.ProfileConfig{Agent: string(parsedAgent), Provider: string(parsedProvider), WireAPI: string(wireAPI), Upstream: upstream, Token: token, TemplateID: template, Namespace: namespace, RemoteUser: remoteUser, RestoreOnExit: restore, Config: configMap, Env: envMap}
		profile, err := agentprofile.ParseProfile(args[0], stored)
		if err != nil {
			return command.Usage(err)
		}
		applyProfileDefaults(&profile)
		if err := appagent.ValidateProfile(profile); err != nil {
			return command.Usage(err)
		}
		stored.TemplateID, stored.Namespace, stored.RemoteUser, stored.Config = profile.TemplateID, profile.Namespace, profile.RemoteUser, profile.Config
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return err
		}
		cfg.AgentProfiles.Profiles[args[0]] = stored
		if use || cfg.AgentProfiles.CurrentProfile == "" {
			cfg.AgentProfiles.CurrentProfile = args[0]
		}
		if err := cliconfig.Save(runtime.Options.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Agent profile saved: %s\n", args[0])
		return nil
	}}
	f := cmd.Flags()
	f.StringVar(&agentType, "agent", "", "codex or claude-code")
	f.StringVar(&provider, "provider", "", "openai or anthropic")
	f.StringVar(&upstream, "upstream", "", "provider base URL")
	f.StringVar(&token, "token", "", "provider token")
	f.StringVar(&template, "template-id", "", "runtime template")
	f.StringVar(&namespace, "namespace", appagent.DefaultNamespace, "namespace")
	f.StringVar(&remoteUser, "remote-user", appagent.DefaultRemoteUser, "container user")
	f.StringVar(&model, "model", "", "default model")
	f.StringArrayVar(&configValues, "agent-config", nil, "agent config KEY=VALUE; may be repeated")
	f.StringArrayVar(&envValues, "env", nil, "remote environment KEY=VALUE; may be repeated")
	f.BoolVar(&restore, "restore-on-exit", false, "restore remote config on exit")
	f.BoolVar(&use, "use", false, "select this profile")
	return cmd
}

func defaultModelConfigKey(agent agentprofile.AgentType) string {
	if agent == agentprofile.AgentClaudeCode {
		return "sonnet_model"
	}
	return "model"
}

func profileGetCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "get <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return err
		}
		value := cfg.AgentProfiles.Profiles[args[0]]
		if value == nil {
			return fmt.Errorf("agent profile not found")
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), profileJSON(args[0], value))
		}
		renderProfile(cmd.OutOrStdout(), args[0], value)
		return nil
	}}
}
func profileListCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return err
		}
		names := cliconfig.AgentProfileNames(cfg)
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), map[string]any{"profiles": names, "current_profile": cfg.AgentProfiles.CurrentProfile})
		}
		for _, name := range names {
			marker := " "
			if name == cfg.AgentProfiles.CurrentProfile {
				marker = "*"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", marker, name)
		}
		return nil
	}}
}
func profileDeleteCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "delete <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return err
		}
		if cfg.AgentProfiles.Profiles[args[0]] == nil {
			return fmt.Errorf("agent profile not found")
		}
		delete(cfg.AgentProfiles.Profiles, args[0])
		if cfg.AgentProfiles.CurrentProfile == args[0] {
			names := cliconfig.AgentProfileNames(cfg)
			sort.Strings(names)
			cfg.AgentProfiles.CurrentProfile = ""
			if len(names) != 0 {
				cfg.AgentProfiles.CurrentProfile = names[0]
			}
		}
		if err := cliconfig.Save(runtime.Options.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Agent profile deleted: %s\n", args[0])
		return nil
	}}
}
func profileUseCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "use <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return err
		}
		if cfg.AgentProfiles.Profiles[args[0]] == nil {
			return fmt.Errorf("agent profile not found")
		}
		cfg.AgentProfiles.CurrentProfile = args[0]
		if err := cliconfig.Save(runtime.Options.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Current agent profile: %s\n", args[0])
		return nil
	}}
}
func keyValues(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("entries must use KEY=VALUE")
		}
		out[strings.TrimSpace(key)] = item
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
