package namespace

import (
	"context"
	appnamespace "github.com/cofy-x/axern/apps/cli/internal/application/namespace"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "namespace", Aliases: []string{"ns"}, Short: "Manage namespaces"}
	for _, operation := range []struct {
		name, short string
		call        func(context.Context, appnamespace.Control, *cobra.Command, string) error
	}{
		{"create", "Create a namespace", func(ctx context.Context, c appnamespace.Control, cmd *cobra.Command, name string) error {
			resp, err := c.Create(ctx, name)
			if err != nil {
				return err
			}
			return renderOne(runtime, cmd, resp.GetNamespace())
		}},
		{"get", "Get a namespace", func(ctx context.Context, c appnamespace.Control, cmd *cobra.Command, name string) error {
			resp, err := c.Get(ctx, name)
			if err != nil {
				return err
			}
			return renderOne(runtime, cmd, resp.GetNamespace())
		}},
		{"delete", "Delete an inactive namespace", func(ctx context.Context, c appnamespace.Control, cmd *cobra.Command, name string) error {
			resp, err := c.Delete(ctx, name)
			if err != nil {
				return err
			}
			return renderOne(runtime, cmd, resp.GetNamespace())
		}},
	} {
		op := operation
		root.AddCommand(&cobra.Command{Use: op.name + " <namespace>", Short: op.short, Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := runtime.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer s.Close()
			return op.call(s.Context, appnamespace.New(s.Clients.Namespace), cmd, args[0])
		}})
	}
	root.AddCommand(&cobra.Command{Use: "list", Short: "List namespaces", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appnamespace.New(s.Clients.Namespace).List(s.Context)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintNamespaceListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderNamespaceTable(cmd.OutOrStdout(), resp.GetNamespaces())
		return nil
	}})
	return root
}

func renderOne(runtime command.Runtime, cmd *cobra.Command, value *namespacev1.Namespace) error {
	if runtime.Options.Output == "json" {
		return output.PrintNamespaceJSON(cmd.OutOrStdout(), value)
	}
	output.RenderNamespace(cmd.OutOrStdout(), value)
	return nil
}
