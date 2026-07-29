package rollout

import (
	"fmt"
	"strings"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/cofy-x/axern/apps/axrun/internal/rolloutspec"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
	"github.com/spf13/cobra"
)

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
		for _, name := range []string{"file", "runner", "attempts", "output-dir"} {
			if cmd.Flags().Changed(name) {
				return command.Usage(fmt.Errorf("--resume cannot be combined with --%s", name))
			}
		}
		runner, err := approllout.ResumeRunner(resume)
		if err != nil {
			return command.Usage(err)
		}
		params := approllout.Params{
			Context:      cmd.Context(),
			ResumeRunDir: resume,
			Execute:      true,
			Concurrency:  command.FlagInt(cmd, "concurrency", 1),
			Attempts:     1,
			Output:       command.FlagString(cmd, "output-dir", ".axrun/runs"),
		}
		if runner == "axern" {
			contextConfig, err := options.ResolveContext()
			if err != nil {
				return command.Usage(err)
			}
			if contextConfig == nil {
				return command.Usage(fmt.Errorf("Axern runner requires a resolved Axern context"))
			}
			params.AxernConfig = command.AxernConfig(contextConfig)
		}
		result, err := (approllout.Service{}).Run(params)
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
			result, err := (approllout.Service{}).Run(params)
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
