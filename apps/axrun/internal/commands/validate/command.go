package validate

import (
	validateapp "github.com/cofy-x/axern/apps/axrun/internal/application/validate"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/spf13/cobra"
)

func Command(options *command.Options) *cobra.Command {
	return &cobra.Command{
		Use: "validate <run-dir>", Short: "Validate a rollout run directory", Args: command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := validateapp.Run(validateapp.Params{RunDir: args[0]})
			if printErr := command.PrintValue(cmd.OutOrStdout(), options.Output, result, "valid=%t problems=%d\n", result.Valid(), len(result.Problems)); printErr != nil {
				return printErr
			}
			return err
		},
	}
}
