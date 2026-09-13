package catalog

import (
	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "catalog", Short: "Inspect environment templates"}
	root.AddCommand(
		&cobra.Command{Use: "list", Short: "List environment templates", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).ListEnvironmentTemplates(s.Context)
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintEnvironmentTemplateListJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderEnvironmentTemplateTable(cmd.OutOrStdout(), resp.GetEnvironmentTemplates())
			return nil
		}},
		&cobra.Command{Use: "get <template-id>", Short: "Get a environment template", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).GetEnvironmentTemplate(s.Context, args[0])
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintEnvironmentTemplateResponseJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderEnvironmentTemplate(cmd.OutOrStdout(), resp.GetEnvironmentTemplate())
			return nil
		}},
	)
	return root
}
