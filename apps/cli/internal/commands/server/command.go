package server

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/serverruntime"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	root := &cobra.Command{
		Use: "server", Short: "Operate the installed native Linux server",
		Long: "Operate the installed native Linux server on this host. The selected API context does not select a server host. Lifecycle operations delegate to the verified installation helper and preserve stored data.",
	}
	for _, operation := range []string{"start", "stop", "status", "renew-services"} {
		operation := operation
		root.AddCommand(&cobra.Command{
			Use: operation, Short: serverHelp(operation), Args: command.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := validateHostOptions(runtime); err != nil {
					return err
				}
				ctx, cancel := operationContext(runtime, cmd.Context())
				defer cancel()
				return serverruntime.New(cmd.OutOrStdout(), cmd.ErrOrStderr()).Operate(ctx, operation)
			},
		})
	}
	options := serverruntime.LogOptions{Tail: 200}
	logs := &cobra.Command{
		Use: "logs [service]", Short: "Read the installed server service journal", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateHostOptions(runtime); err != nil {
				return err
			}
			if len(args) != 0 {
				options.Service = args[0]
			}
			if options.Tail < 0 || options.Tail > 10000 {
				return command.Usage(fmt.Errorf("--tail must be in [0, 10000]"))
			}
			ctx, cancel := operationContext(runtime, cmd.Context())
			defer cancel()
			return serverruntime.New(cmd.OutOrStdout(), cmd.ErrOrStderr()).Logs(ctx, options)
		},
	}
	logs.Flags().BoolVarP(&options.Follow, "follow", "f", false, "follow the service journal")
	logs.Flags().IntVar(&options.Tail, "tail", options.Tail, "maximum recent journal entries")
	root.AddCommand(logs)
	return root
}

func validateHostOptions(runtime command.Runtime) error {
	if runtime.Options != nil && runtime.Options.Output != "" && runtime.Options.Output != "table" {
		return command.Usage(fmt.Errorf("server operations stream host tool output; --output must be table"))
	}
	if runtime.Root != nil {
		for _, flag := range []string{"context", "endpoint", "tls-ca-cert", "tls-cert", "tls-key", "tls-server-name", "proxy-mode"} {
			if runtime.Root.PersistentFlags().Changed(flag) {
				return command.Usage(fmt.Errorf("--%s selects API access and cannot select the host for server operations", flag))
			}
		}
	}
	return nil
}

func operationContext(runtime command.Runtime, parent context.Context) (context.Context, context.CancelFunc) {
	if runtime.Options != nil && runtime.Options.Timeout > 0 {
		return context.WithTimeout(parent, runtime.Options.Timeout)
	}
	return parent, func() {}
}

func serverHelp(operation string) string {
	switch operation {
	case "start":
		return "Start services and wait for authenticated serving readiness"
	case "stop":
		return "Stop the complete installation and retain stored data"
	case "status":
		return "Show installed systemd service status"
	case "renew-services":
		return "Renew installed service identities without replacing authority"
	default:
		return operation
	}
}
