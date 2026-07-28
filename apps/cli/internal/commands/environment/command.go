package environment

import (
	"context"
	"fmt"

	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "environment", Short: "Manage immutable environments"}
	root.AddCommand(createCommand(runtime), getCommand(runtime), listCommand(runtime), deleteCommand(runtime))
	return root
}

func createCommand(runtime command.Runtime) *cobra.Command {
	var namespace, templateID, templateVersion, imageRef, credentialID string
	var labels []string
	var readonly bool
	cmd := &cobra.Command{Use: "create", Short: "Create an environment", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var spec *environmentv1.EnvironmentSpec
		switch {
		case imageRef != "" && templateID != "":
			return command.Usage(fmt.Errorf("template-id cannot be combined with image-ref"))
		case imageRef != "":
			spec = &environmentv1.EnvironmentSpec{Namespace: namespace, Image: &environmentv1.EnvironmentImageSource{Ref: imageRef, RegistryCredentialID: credentialID, RootfsReadonly: readonly}}
		case templateID != "":
			spec = &environmentv1.EnvironmentSpec{Namespace: namespace, TemplateID: templateID, TemplateVersion: templateVersion}
		default:
			return command.Usage(fmt.Errorf("template-id or image-ref is required"))
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appenvironment.New(s.Clients.Environment).Create(s.Context, appenvironment.CreateParams{Spec: spec, Labels: parse.Labels(labels)})
		if err != nil {
			return err
		}
		return renderEnvironment(runtime, cmd, resp.GetEnvironment())
	}}
	f := cmd.Flags()
	f.StringVar(&namespace, "namespace", "default", "namespace")
	f.StringVar(&templateID, "template-id", "", "runtime template id")
	f.StringVar(&templateVersion, "template-version", "", "runtime template version")
	f.StringVar(&imageRef, "image-ref", "", "OCI image reference")
	f.StringVar(&credentialID, "registry-credential-id", "", "registry credential secret id")
	f.BoolVar(&readonly, "rootfs-readonly", false, "mount rootfs read-only")
	f.StringArrayVar(&labels, "label", nil, "label key=value; may be repeated")
	return cmd
}

func getCommand(runtime command.Runtime) *cobra.Command {
	return environmentOne(runtime, "get", func(ctx context.Context, c appenvironment.Control, id string) (*environmentv1.Environment, error) {
		r, err := c.Get(ctx, id)
		return r.GetEnvironment(), err
	})
}
func deleteCommand(runtime command.Runtime) *cobra.Command {
	return environmentOne(runtime, "delete", func(ctx context.Context, c appenvironment.Control, id string) (*environmentv1.Environment, error) {
		r, err := c.Delete(ctx, id)
		return r.GetEnvironment(), err
	})
}

func environmentOne(runtime command.Runtime, name string, call func(context.Context, appenvironment.Control, string) (*environmentv1.Environment, error)) *cobra.Command {
	return &cobra.Command{Use: name + " <environment-id>", Short: name + " an environment", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		value, err := call(s.Context, appenvironment.NewWithDelete(s.Clients.Environment), args[0])
		if err != nil {
			return err
		}
		return renderEnvironment(runtime, cmd, value)
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List environments", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appenvironment.New(s.Clients.Environment).List(s.Context)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintEnvironmentListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderEnvironmentTable(cmd.OutOrStdout(), resp.GetEnvironments())
		return nil
	}}
}

func renderEnvironment(runtime command.Runtime, cmd *cobra.Command, value *environmentv1.Environment) error {
	if runtime.Options.Output == "json" {
		return output.PrintEnvironmentResponseJSON(cmd.OutOrStdout(), value)
	}
	output.RenderEnvironment(cmd.OutOrStdout(), value)
	return nil
}
