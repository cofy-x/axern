package catalog

import (
	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "catalog", Short: "Inspect runtime templates"}
	root.AddCommand(
		&cobra.Command{Use: "list", Short: "List runtime templates", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).ListRuntimeTemplates(s.Context)
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintRuntimeTemplateListJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderRuntimeTemplateTable(cmd.OutOrStdout(), resp.GetRuntimeTemplates())
			return nil
		}},
		&cobra.Command{Use: "get <template-id>", Short: "Get a runtime template", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).GetRuntimeTemplate(s.Context, args[0])
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintRuntimeTemplateResponseJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderRuntimeTemplate(cmd.OutOrStdout(), resp.GetRuntimeTemplate())
			return nil
		}},
	)
	bundles := &cobra.Command{Use: "bundle", Short: "Inspect agent bundles"}
	bundles.AddCommand(
		&cobra.Command{Use: "list", Short: "List agent bundles", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).ListAgentBundles(s.Context)
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintAgentBundleListJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderAgentBundleTable(cmd.OutOrStdout(), resp.GetAgentBundles())
			return nil
		}},
		&cobra.Command{Use: "get <bundle-id>", Short: "Get an agent bundle", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			resp, err := appcatalog.New(s.Clients.Catalog).GetAgentBundle(s.Context, args[0])
			if err != nil {
				return err
			}
			if runtime.Options.Output == "json" {
				return output.PrintAgentBundleResponseJSON(cmd.OutOrStdout(), resp)
			}
			output.RenderAgentBundle(cmd.OutOrStdout(), resp.GetAgentBundle())
			return nil
		}},
	)
	root.AddCommand(bundles)
	return root
}
