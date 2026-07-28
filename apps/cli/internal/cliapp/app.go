package cliapp

import (
	"os"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	admincmd "github.com/cofy-x/axern/apps/cli/internal/commands/admin"
	agentcmd "github.com/cofy-x/axern/apps/cli/internal/commands/agent"
	"github.com/cofy-x/axern/apps/cli/internal/commands/catalog"
	contextcmd "github.com/cofy-x/axern/apps/cli/internal/commands/context"
	"github.com/cofy-x/axern/apps/cli/internal/commands/dashboard"
	"github.com/cofy-x/axern/apps/cli/internal/commands/environment"
	functioncmd "github.com/cofy-x/axern/apps/cli/internal/commands/function"
	namespacecmd "github.com/cofy-x/axern/apps/cli/internal/commands/namespace"
	"github.com/cofy-x/axern/apps/cli/internal/commands/quota"
	"github.com/cofy-x/axern/apps/cli/internal/commands/run"
	"github.com/cofy-x/axern/apps/cli/internal/commands/secret"
	"github.com/cofy-x/axern/apps/cli/internal/commands/service"
	sshcmd "github.com/cofy-x/axern/apps/cli/internal/commands/ssh"
	tunnelcmd "github.com/cofy-x/axern/apps/cli/internal/commands/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/config"
	"github.com/spf13/cobra"
)

func New(version string) *cobra.Command {
	configPath := os.Getenv("AXERN_CONFIG")
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	options := &command.Options{ConfigPath: configPath, Output: "table"}
	root := &cobra.Command{
		Use:           "axern",
		Short:         "Manage Axern resources and interactive development workflows",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return command.Usage(err) })
	flags := root.PersistentFlags()
	flags.StringVar(&options.ConfigPath, "config", configPath, "path to the Axern context file")
	flags.StringVar(&options.ContextName, "context", os.Getenv("AXERN_CONTEXT"), "Axern context name")
	flags.StringVar(&options.Endpoint, "endpoint", "", "gateway gRPC endpoint")
	flags.StringVar(&options.TLSCACert, "tls-ca-cert", "", "gateway CA certificate")
	flags.StringVar(&options.TLSCert, "tls-cert", "", "client certificate")
	flags.StringVar(&options.TLSKey, "tls-key", "", "client private key")
	flags.StringVar(&options.TLSServerName, "tls-server-name", "", "TLS server name")
	flags.StringVar(&options.ProxyMode, "proxy-mode", "", "proxy mode: env or direct")
	flags.DurationVar(&options.Timeout, "timeout", 0, "overall command timeout; 0 disables it")
	flags.StringVarP(&options.Output, "output", "o", "table", "output format: table or json")
	runtime := command.Runtime{Options: options, Root: root}
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		return runtime.ValidateOutput()
	}
	root.AddCommand(
		contextcmd.Command(runtime),
		admincmd.Command(runtime),
		catalog.Command(runtime),
		environment.Command(runtime),
		functioncmd.Command(runtime),
		namespacecmd.Command(runtime),
		secret.Command(runtime),
		run.Command(runtime),
		service.Command(runtime),
		quota.Command(runtime),
		tunnelcmd.Command(runtime),
		sshcmd.Command(runtime),
		dashboard.Command(runtime),
		agentcmd.Command(runtime),
	)
	root.InitDefaultCompletionCmd()
	return root
}

func Execute(root *cobra.Command, args []string) error {
	root.SetArgs(args)
	if _, _, err := root.Find(args); err != nil {
		return command.Usage(err)
	}
	return root.Execute()
}
