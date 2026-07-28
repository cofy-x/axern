package secret

import (
	"context"
	"fmt"
	"os"
	"strings"

	appsecret "github.com/cofy-x/axern/apps/cli/internal/application/secret"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/parse"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{Use: "secret", Short: "Manage secrets"}
	root.AddCommand(createCommand(runtime), getCommand(runtime), listCommand(runtime), deleteCommand(runtime))
	return root
}

func createCommand(runtime command.Runtime) *cobra.Command {
	var namespace, kind, file string
	var literals, labels []string
	cmd := &cobra.Command{Use: "create", Short: "Create an immutable secret", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		secretType, err := parse.SecretType(kind)
		if err != nil {
			return command.Usage(err)
		}
		var data map[string]string
		switch secretType {
		case secretv1.SecretType_SECRET_TYPE_OPAQUE:
			if file != "" {
				return command.Usage(fmt.Errorf("--file requires --type docker-config-json"))
			}
			data = parse.Labels(literals)
			if len(data) == 0 {
				return command.Usage(fmt.Errorf("at least one --literal is required"))
			}
		case secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON:
			if len(literals) != 0 || file == "" {
				return command.Usage(fmt.Errorf("docker-config-json requires --file and forbids --literal"))
			}
			content, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			data = map[string]string{".dockerconfigjson": string(content)}
		}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appsecret.New(s.Clients.Secret).Create(s.Context, &secretv1.CreateSecretRequest{Namespace: strings.TrimSpace(namespace), Type: secretType, StringData: data, Labels: parse.Labels(labels)})
		if err != nil {
			return err
		}
		return renderSecret(runtime, cmd, resp.GetSecret())
	}}
	f := cmd.Flags()
	f.StringVar(&namespace, "namespace", "default", "namespace")
	f.StringVar(&kind, "type", "opaque", "secret type")
	f.StringArrayVar(&literals, "literal", nil, "secret key=value; may be repeated")
	f.StringVar(&file, "file", "", "secret source file")
	f.StringArrayVar(&labels, "label", nil, "label key=value; may be repeated")
	return cmd
}

func getCommand(runtime command.Runtime) *cobra.Command {
	return secretOne(runtime, "get", func(ctx context.Context, c appsecret.Control, id string) (*secretv1.Secret, error) {
		r, err := c.Get(ctx, id)
		return r.GetSecret(), err
	})
}
func deleteCommand(runtime command.Runtime) *cobra.Command {
	return secretOne(runtime, "delete", func(ctx context.Context, c appsecret.Control, id string) (*secretv1.Secret, error) {
		r, err := c.Delete(ctx, id)
		return r.GetSecret(), err
	})
}
func secretOne(runtime command.Runtime, name string, call func(context.Context, appsecret.Control, string) (*secretv1.Secret, error)) *cobra.Command {
	return &cobra.Command{Use: name + " <secret-id>", Short: name + " a secret", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		value, err := call(s.Context, appsecret.New(s.Clients.Secret), args[0])
		if err != nil {
			return err
		}
		return renderSecret(runtime, cmd, value)
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, kind, cursor string
	var pageSize int32
	cmd := &cobra.Command{Use: "list", Short: "List secret metadata", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		secretType, err := parse.SecretTypeAllowEmpty(kind)
		if err != nil {
			return command.Usage(err)
		}
		req := &secretv1.ListSecretsRequest{Filter: &secretv1.SecretListFilter{Namespace: namespace, Type: secretType, Cursor: cursor, PageSize: pageSize}}
		s, err := runtime.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer s.Close()
		resp, err := appsecret.New(s.Clients.Secret).List(s.Context, req)
		if err != nil {
			return err
		}
		if runtime.Options.Output == "json" {
			return output.PrintSecretListJSON(cmd.OutOrStdout(), resp)
		}
		output.RenderSecretTable(cmd.OutOrStdout(), resp.GetSecrets())
		return nil
	}}
	f := cmd.Flags()
	f.StringVar(&namespace, "namespace", "", "namespace filter")
	f.StringVar(&kind, "type", "", "secret type filter")
	f.StringVar(&cursor, "cursor", "", "pagination cursor")
	f.Int32Var(&pageSize, "page-size", 0, "page size")
	return cmd
}

func renderSecret(runtime command.Runtime, cmd *cobra.Command, value *secretv1.Secret) error {
	if runtime.Options.Output == "json" {
		return output.PrintSecretResponseJSON(cmd.OutOrStdout(), value)
	}
	output.RenderSecret(cmd.OutOrStdout(), value)
	return nil
}
