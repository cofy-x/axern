package profile

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/command"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const maxCredentialBytes = 16 << 10

func Command(options *command.Options) *cobra.Command {
	root := &cobra.Command{Use: "profile", Short: "Manage namespace-scoped agent profiles"}
	root.AddCommand(create(options), get(options), list(options), update(options), rotate(options), doctor(options), deleteProfile(options))
	return root
}

func api(cmd *cobra.Command, options *command.Options) (agentprofilev1.AgentProfileControlClient, io.Closer, error) {
	client, err := options.ControlClient(cmd.Context())
	if err != nil {
		return nil, nil, err
	}
	return client.AgentProfileControl(), client, nil
}

type tokenFlags struct {
	stdin bool
	env   string
}

func (f *tokenFlags) bind(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.stdin, "token-stdin", false, "read provider token from stdin")
	cmd.Flags().StringVar(&f.env, "token-env", "", "read provider token from the named environment variable")
}

func (f tokenFlags) read(in io.Reader) ([]byte, error) {
	if f.stdin == (strings.TrimSpace(f.env) != "") {
		return nil, command.Usage(fmt.Errorf("exactly one of --token-stdin or --token-env is required"))
	}
	var value []byte
	var err error
	if f.stdin {
		value, err = io.ReadAll(io.LimitReader(in, maxCredentialBytes+1))
	} else {
		raw, ok := os.LookupEnv(strings.TrimSpace(f.env))
		if !ok {
			return nil, fmt.Errorf("token environment variable %q is not set", f.env)
		}
		value = []byte(raw)
	}
	if err != nil {
		return nil, err
	}
	value = []byte(strings.TrimSpace(string(value)))
	if len(value) == 0 {
		return nil, fmt.Errorf("provider token is empty")
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("provider token exceeds 16 KiB limit")
	}
	return value, nil
}

func create(options *command.Options) *cobra.Command {
	var namespace, agent, provider, wire, base string
	var concurrency int32
	var token tokenFlags
	var key string
	cmd := &cobra.Command{Use: "create <name>", Short: "Create an agent profile with an owned encrypted credential", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		credential, err := token.read(cmd.InOrStdin())
		if err != nil {
			return err
		}
		spec, err := profileSpec(profileSpecInput{
			Agent:          agent,
			Provider:       provider,
			WireAPI:        wire,
			BaseURL:        base,
			MaxConcurrency: concurrency,
		})
		if err != nil {
			return command.Usage(err)
		}
		client, closer, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer closer.Close()
		if key == "" {
			key = uuid.NewString()
		}
		response, err := client.CreateAgentProfile(cmd.Context(), &agentprofilev1.CreateAgentProfileRequest{
			Namespace:      namespace,
			Name:           args[0],
			Spec:           spec,
			Credential:     credential,
			IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		return print(cmd, options, response.GetProfile())
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "profile namespace")
	cmd.Flags().StringVar(&agent, "agent", "", "agent name")
	cmd.Flags().StringVar(&provider, "provider", "", "provider: openai or anthropic")
	cmd.Flags().StringVar(&wire, "wire-api", "", "wire API: responses or anthropic-messages")
	cmd.Flags().StringVar(&base, "base-url", "", "provider base URL")
	cmd.Flags().Int32Var(&concurrency, "max-concurrency", 1, "maximum concurrent provider operations")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "stable operation idempotency key")
	token.bind(cmd)
	return cmd
}

type profileSpecInput struct {
	Agent          string
	Provider       string
	WireAPI        string
	BaseURL        string
	MaxConcurrency int32
}

func profileSpec(input profileSpecInput) (*agentprofilev1.AgentProfileSpec, error) {
	providers := map[string]agentprofilev1.AgentProvider{
		"openai":    agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI,
		"anthropic": agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC,
	}
	wires := map[string]agentprofilev1.AgentWireApi{
		"responses":          agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES,
		"anthropic-messages": agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES,
	}
	p, ok := providers[strings.ToLower(strings.TrimSpace(input.Provider))]
	if !ok {
		return nil, fmt.Errorf("provider must be openai or anthropic")
	}
	w, ok := wires[strings.ToLower(strings.TrimSpace(input.WireAPI))]
	if !ok {
		return nil, fmt.Errorf("wire-api must be responses or anthropic-messages")
	}
	return &agentprofilev1.AgentProfileSpec{
		Agent:          strings.TrimSpace(input.Agent),
		Provider:       p,
		WireApi:        w,
		BaseUrl:        strings.TrimSpace(input.BaseURL),
		MaxConcurrency: input.MaxConcurrency,
	}, nil
}

func get(options *command.Options) *cobra.Command {
	var ns string
	cmd := &cobra.Command{Use: "get <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		r, err := api.GetAgentProfile(cmd.Context(), &agentprofilev1.GetAgentProfileRequest{Namespace: ns, Name: args[0]})
		if err != nil {
			return err
		}
		return print(cmd, options, r.GetProfile())
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	return cmd
}

func list(options *command.Options) *cobra.Command {
	var ns string
	cmd := &cobra.Command{Use: "list", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		r, err := api.ListAgentProfiles(cmd.Context(), &agentprofilev1.ListAgentProfilesRequest{
			Filter: &agentprofilev1.AgentProfileListFilter{Namespace: ns},
		})
		if err != nil {
			return err
		}
		if options.Output == "json" {
			return command.PrintValue(cmd.OutOrStdout(), "json", r, "")
		}
		for _, p := range r.GetProfiles() {
			if err := print(cmd, options, p); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	return cmd
}

func update(options *command.Options) *cobra.Command {
	var ns, base, key string
	var concurrency int32
	var expected int64
	cmd := &cobra.Command{Use: "update <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		if expected == 0 {
			current, err := api.GetAgentProfile(cmd.Context(), &agentprofilev1.GetAgentProfileRequest{
				Namespace: ns,
				Name:      args[0],
			})
			if err != nil {
				return err
			}
			expected = current.GetProfile().GetVersion()
		}
		patch := &agentprofilev1.AgentProfilePatch{}
		if cmd.Flags().Changed("base-url") {
			patch.BaseUrl = &base
		}
		if cmd.Flags().Changed("max-concurrency") {
			patch.MaxConcurrency = &concurrency
		}
		if patch.BaseUrl == nil && patch.MaxConcurrency == nil {
			return command.Usage(fmt.Errorf("at least one update flag is required"))
		}
		if key == "" {
			key = uuid.NewString()
		}
		r, err := api.UpdateAgentProfile(cmd.Context(), &agentprofilev1.UpdateAgentProfileRequest{
			Namespace:       ns,
			Name:            args[0],
			Patch:           patch,
			ExpectedVersion: expected,
			IdempotencyKey:  key,
		})
		if err != nil {
			return err
		}
		return print(cmd, options, r.GetProfile())
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	cmd.Flags().StringVar(&base, "base-url", "", "provider base URL")
	cmd.Flags().Int32Var(&concurrency, "max-concurrency", 0, "maximum concurrent provider operations")
	cmd.Flags().Int64Var(&expected, "expected-version", 0, "expected profile version (defaults to current)")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "stable operation idempotency key")
	return cmd
}

func rotate(options *command.Options) *cobra.Command {
	var ns, key string
	var expected int64
	var token tokenFlags
	cmd := &cobra.Command{Use: "rotate <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		credential, err := token.read(cmd.InOrStdin())
		if err != nil {
			return err
		}
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		if expected == 0 {
			current, err := api.GetAgentProfile(cmd.Context(), &agentprofilev1.GetAgentProfileRequest{
				Namespace: ns,
				Name:      args[0],
			})
			if err != nil {
				return err
			}
			expected = current.GetProfile().GetVersion()
		}
		if key == "" {
			key = uuid.NewString()
		}
		r, err := api.RotateAgentProfileCredential(cmd.Context(), &agentprofilev1.RotateAgentProfileCredentialRequest{
			Namespace:       ns,
			Name:            args[0],
			Credential:      credential,
			ExpectedVersion: expected,
			IdempotencyKey:  key,
		})
		if err != nil {
			return err
		}
		return print(cmd, options, r.GetProfile())
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	cmd.Flags().Int64Var(&expected, "expected-version", 0, "expected profile version (defaults to current)")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "stable operation idempotency key")
	token.bind(cmd)
	return cmd
}

func doctor(options *command.Options) *cobra.Command {
	var ns, model string
	cmd := &cobra.Command{Use: "doctor <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(model) == "" {
			return command.Usage(fmt.Errorf("--model is required"))
		}
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		r, err := api.DoctorAgentProfile(cmd.Context(), &agentprofilev1.DoctorAgentProfileRequest{
			Namespace: ns,
			Name:      args[0],
			Model:     model,
		})
		if err != nil {
			return err
		}
		if err := command.PrintValue(cmd.OutOrStdout(), options.Output, r, "profile=%s healthy=%t checks=%d\n", r.GetProfile().GetName(), r.GetHealthy(), len(r.GetChecks())); err != nil {
			return err
		}
		if !r.GetHealthy() {
			return command.Exit(14, fmt.Errorf("profile doctor reported unhealthy provider checks"))
		}
		return nil
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	cmd.Flags().StringVar(&model, "model", "", "provider model to probe")
	return cmd
}

func deleteProfile(options *command.Options) *cobra.Command {
	var ns string
	var expected int64
	cmd := &cobra.Command{Use: "delete <name>", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, close, err := api(cmd, options)
		if err != nil {
			return err
		}
		defer close.Close()
		if expected == 0 {
			current, err := api.GetAgentProfile(cmd.Context(), &agentprofilev1.GetAgentProfileRequest{
				Namespace: ns,
				Name:      args[0],
			})
			if err != nil {
				return err
			}
			expected = current.GetProfile().GetVersion()
		}
		r, err := api.DeleteAgentProfile(cmd.Context(), &agentprofilev1.DeleteAgentProfileRequest{
			Namespace:       ns,
			Name:            args[0],
			ExpectedVersion: expected,
		})
		if err != nil {
			return err
		}
		return print(cmd, options, r.GetProfile())
	}}
	cmd.Flags().StringVarP(&ns, "namespace", "n", "default", "profile namespace")
	cmd.Flags().Int64Var(&expected, "expected-version", 0, "expected profile version (defaults to current)")
	return cmd
}

func print(cmd *cobra.Command, options *command.Options, p *agentprofilev1.AgentProfile) error {
	return command.PrintValue(cmd.OutOrStdout(), options.Output, p, "%s\t%s\tv%d\tcredential-v%d\t%s\n", p.GetName(), p.GetSpec().GetAgent(), p.GetVersion(), p.GetCredentialVersion(), p.GetUpdatedAt().AsTime().Format("2006-01-02T15:04:05Z07:00"))
}
