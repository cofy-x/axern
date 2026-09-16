package rollout

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agentprofile"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/rolloutspec"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	"github.com/spf13/cobra"
)

func Command(options *command.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Plan or execute reproducible agent episodes",
	}
	cmd.AddCommand(Plan(options), Run(options))
	return cmd
}

func Plan(options *command.Options) *cobra.Command {
	return rolloutCommand(options, rolloutCommandConfig{Use: "plan"})
}

func Run(options *command.Options) *cobra.Command {
	cmd := rolloutCommand(options, rolloutCommandConfig{Use: "run", Execute: true})
	var resume string
	cmd.Flags().StringVar(&resume, "resume", "", "resume an existing run directory")
	original := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if resume == "" {
			return original(cmd, args)
		}
		for _, name := range []string{"file", "runner", "concurrency", "attempts", "output-dir"} {
			if cmd.Flags().Changed(name) {
				return command.Usage(fmt.Errorf("--resume cannot be combined with --%s", name))
			}
		}
		descriptor, err := approllout.DescribeResume(resume)
		if err != nil {
			return command.Usage(err)
		}
		service, provider, err := rolloutService(descriptor.Profile)
		if err != nil {
			return command.Usage(err)
		}
		params := approllout.Params{
			Context:             cmd.Context(),
			ResumeRunDir:        resume,
			Execute:             true,
			Attempts:            1,
			Output:              command.FlagString(cmd, "output-dir", ".axrun/runs"),
			ProviderRequirement: provider,
		}
		if descriptor.Runner == "axern" {
			contextConfig, err := options.ResolveContext()
			if err != nil {
				return command.Usage(err)
			}
			if contextConfig == nil {
				return command.Usage(fmt.Errorf("Axern runner requires a resolved Axern context"))
			}
			params.AxernConfig = command.AxernConfig(contextConfig)
		}
		result, err := service.Run(params)
		if err != nil {
			return err
		}
		return command.PrintRollout(cmd.OutOrStdout(), options.Output, result)
	}
	return cmd
}

type rolloutCommandConfig struct {
	Use     string
	Execute bool
}

func rolloutCommand(options *command.Options, config rolloutCommandConfig) *cobra.Command {
	var file, runner, outputDir string
	var concurrency, attempts int
	cmd := &cobra.Command{
		Use:   config.Use,
		Short: map[bool]string{true: "Execute a rollout", false: "Create an immutable rollout plan"}[config.Execute],
		Args:  command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(file) == "" {
				return command.Usage(fmt.Errorf("--file is required"))
			}
			spec, err := rolloutspec.Load(file)
			if err != nil {
				return command.Usage(err)
			}
			var contextConfig *clientconfig.Context
			if spec.Runner(runner) == "axern" {
				contextConfig, err = options.ResolveContext()
				if err != nil {
					return command.Usage(err)
				}
			}
			params, err := spec.Params(config.Execute, rolloutspec.Overrides{
				Runner:      runner,
				Concurrency: concurrency,
				Attempts:    attempts,
				OutputDir:   outputDir,
				Context:     contextConfig,
			})
			if err != nil {
				return command.Usage(err)
			}
			params.Context = cmd.Context()
			service, provider, err := rolloutService(params.AgentProfile)
			if err != nil {
				return command.Usage(err)
			}
			params.ProviderRequirement = provider
			result, err := service.Run(params)
			if err != nil {
				return err
			}
			return command.PrintRollout(cmd.OutOrStdout(), options.Output, result)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to an axrun/v1 Rollout spec")
	cmd.Flags().StringVar(&runner, "runner", "", "runner override: axern or local")
	cmd.Flags().IntVarP(&concurrency, "concurrency", "n", 0, "concurrency override")
	cmd.Flags().IntVar(&attempts, "attempts", 0, "attempt count override")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "run output directory override")
	return cmd
}

func rolloutService(profileName string) (approllout.Service, *domain.ProviderRequirement, error) {
	if strings.TrimSpace(profileName) == "" {
		return approllout.Service{}, nil, nil
	}
	name, profile, ok, err := agentprofile.Resolve("", profileName)
	if err != nil {
		return approllout.Service{}, nil, err
	}
	if !ok {
		return approllout.Service{}, nil, fmt.Errorf("agent profile %q not found", name)
	}
	snapshot, err := agentprofile.Snapshot(profile)
	if err != nil {
		return approllout.Service{}, nil, err
	}
	provider := &domain.ProviderRequirement{
		Agent: snapshot.Agent, Provider: snapshot.Provider, WireAPI: snapshot.WireAPI,
		Endpoint: snapshot.Endpoint, ConfigFingerprint: snapshot.ConfigFingerprint,
	}
	registry := agentcatalog.RegistryWithProfiles(map[string]agentprofile.Profile{name: profile})
	return approllout.Service{AgentRegistry: registry}, provider, nil
}
