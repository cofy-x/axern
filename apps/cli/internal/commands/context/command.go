package contextcmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	cliconfig "github.com/cofy-x/axern/apps/cli/internal/config"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/lib/go/clientconfig"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	cmd := &cobra.Command{Use: "context", Aliases: []string{"ctx"}, Short: "Manage Axern contexts"}
	cmd.AddCommand(currentCommand(runtime), listCommand(runtime), useCommand(runtime), setCommand(runtime), importKubernetesCommand(runtime))
	return cmd
}

func importKubernetesCommand(runtime command.Runtime) *cobra.Command {
	var namespace, secretName, kubeconfig, kubeContext string
	var params cliconfig.KubernetesImportParams
	cmd := &cobra.Command{
		Use:   "import-kubernetes <name>",
		Short: "Import a context from an Axern Kubernetes release",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params.Name = args[0]
			params.ConfigPath = runtime.Options.ConfigPath
			if params.CertDir == "" {
				params.CertDir = filepath.Join(filepath.Dir(params.ConfigPath), "contexts", params.Name)
			}
			kubectlArgs := make([]string, 0, 12)
			if kubeconfig != "" {
				kubectlArgs = append(kubectlArgs, "--kubeconfig", kubeconfig)
			}
			if kubeContext != "" {
				kubectlArgs = append(kubectlArgs, "--context", kubeContext)
			}
			kubectlArgs = append(kubectlArgs, "--namespace", namespace, "get", "secret", secretName, "--output", "json")
			process := exec.CommandContext(cmd.Context(), "kubectl", kubectlArgs...)
			var stdout, stderr bytes.Buffer
			process.Stdout = &stdout
			process.Stderr = &stderr
			if err := process.Run(); err != nil {
				message := strings.TrimSpace(stderr.String())
				if message == "" {
					message = err.Error()
				}
				return fmt.Errorf("read Kubernetes Secret %s/%s: %s", namespace, secretName, message)
			}
			if err := cliconfig.ImportKubernetesSecret(stdout.Bytes(), params); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported context %s\n", params.Name)
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&namespace, "namespace", "axern-system", "Kubernetes namespace")
	flags.StringVar(&secretName, "secret", "controld-pki", "Axern PKI Secret name")
	flags.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig")
	flags.StringVar(&kubeContext, "kube-context", "", "Kubernetes context name")
	flags.StringVar(&params.CertDir, "cert-dir", "", "directory for imported certificates")
	flags.StringVar(&params.Endpoint, "endpoint", "127.0.0.1:25100", "gateway gRPC endpoint")
	flags.StringVar(&params.ServiceURL, "service-url", "http://127.0.0.1:25101", "gateway HTTP service URL")
	flags.StringVar(&params.SSHEndpoint, "ssh-endpoint", "127.0.0.1:25122", "gateway SSH endpoint")
	flags.StringVar(&params.SSHIdentityFile, "ssh-identity-file", "", "gateway SSH identity file")
	flags.StringVar(&params.TLSServerName, "tls-server-name", "", "gateway TLS server name")
	flags.StringVar(&params.ProxyMode, "proxy-mode", clientconfig.ProxyModeDirect, "proxy mode: env or direct")
	flags.BoolVar(&params.Current, "current", true, "select this context")
	return cmd
}

func currentCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "current", Short: "Show the active context", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return command.Usage(err)
		}
		name := runtime.Options.ContextName
		if name == "" {
			name = cfg.CurrentContext
		}
		profile := cfg.Contexts[name]
		if name == "" || profile == nil {
			return fmt.Errorf("no active Axern context configured")
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), struct {
				Name    string                `json:"name"`
				Context *clientconfig.Context `json:"context"`
			}{name, profile})
		}
		rows := []output.ContextListRow{{Active: true, Name: name, Endpoint: profile.Endpoint, ServiceURL: profile.ServiceURL, SSHEndpoint: profile.SSHEndpoint, ProxyMode: profile.ProxyMode}}
		output.RenderContextTable(cmd.OutOrStdout(), rows)
		return nil
	}}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List configured contexts", Args: command.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return command.Usage(err)
		}
		active := runtime.Options.ContextName
		if active == "" {
			active = cfg.CurrentContext
		}
		rows := make([]output.ContextListRow, 0, len(cfg.Contexts))
		for _, name := range cliconfig.ContextNames(cfg) {
			profile := cfg.Contexts[name]
			rows = append(rows, output.ContextListRow{Active: name == active, Name: name, Endpoint: profile.Endpoint, ServiceURL: profile.ServiceURL, SSHEndpoint: profile.SSHEndpoint, ProxyMode: profile.ProxyMode})
		}
		if runtime.Options.Output == "json" {
			return output.PrintJSON(cmd.OutOrStdout(), rows)
		}
		output.RenderContextTable(cmd.OutOrStdout(), rows)
		return nil
	}}
}

func useCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{Use: "use <name>", Short: "Select the active context", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return command.Usage(err)
		}
		if cfg.Contexts[args[0]] == nil {
			return fmt.Errorf("Axern context %q not found", args[0])
		}
		cfg.CurrentContext = args[0]
		if err := cliconfig.Save(runtime.Options.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Current context: %s\n", args[0])
		if env := os.Getenv("AXERN_CONTEXT"); env != "" && env != args[0] {
			fmt.Fprintf(cmd.ErrOrStderr(), "AXERN_CONTEXT=%s overrides the persisted context.\n", env)
		}
		return nil
	}}
}

func setCommand(runtime command.Runtime) *cobra.Command {
	var value clientconfig.Context
	var current bool
	cmd := &cobra.Command{Use: "set <name>", Short: "Create or replace a context", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		value.ProxyMode = strings.TrimSpace(value.ProxyMode)
		if err := clientconfig.Validate(&value); err != nil {
			return command.Usage(err)
		}
		cfg, err := cliconfig.Load(runtime.Options.ConfigPath)
		if err != nil {
			return command.Usage(err)
		}
		cfg.Contexts[args[0]] = &value
		if current || cfg.CurrentContext == "" {
			cfg.CurrentContext = args[0]
		}
		if err := cliconfig.Save(runtime.Options.ConfigPath, cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Saved context %s\n", args[0])
		return nil
	}}
	flags := cmd.Flags()
	flags.StringVar(&value.Endpoint, "endpoint", "", "gateway gRPC endpoint")
	flags.StringVar(&value.ServiceURL, "service-url", "", "gateway HTTP service URL")
	flags.StringVar(&value.SSHEndpoint, "ssh-endpoint", "", "gateway SSH endpoint")
	flags.StringVar(&value.SSHIdentityFile, "ssh-identity-file", "", "gateway SSH identity file")
	flags.StringVar(&value.TLS.CACert, "tls-ca-cert", "", "gateway CA certificate")
	flags.StringVar(&value.TLS.Cert, "tls-cert", "", "client certificate")
	flags.StringVar(&value.TLS.Key, "tls-key", "", "client private key")
	flags.StringVar(&value.TLS.ServerName, "tls-server-name", "", "TLS server name")
	flags.StringVar(&value.ProxyMode, "proxy-mode", clientconfig.ProxyModeEnv, "proxy mode: env or direct")
	flags.BoolVar(&current, "current", false, "select this context")
	return cmd
}
