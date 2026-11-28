package preview

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/application/preview"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	var port uint32
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "preview <allocation-id>",
		Short: "Open a temporary local browser preview of an Allocation HTTP service",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := preview.Validate(port, ttl); err != nil {
				return command.Usage(err)
			}
			signalCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(signalCtx, ttl)
			defer cancel()
			session, err := runtime.Open(ctx)
			if err != nil {
				return err
			}
			defer session.Close()
			client, err := axern.NewClient(session.Context, "", axern.WithControlConn(session.Conn))
			if err != nil {
				return err
			}
			target, err := client.Allocation(args[0])
			if err != nil {
				return command.Usage(err)
			}
			return preview.Serve(session.Context, target, port, func(address string) error {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), address)
				return err
			})
		},
	}
	cmd.Flags().Uint32Var(&port, "port", 0, "guest loopback HTTP port")
	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Minute, "preview lifetime, up to one hour")
	return cmd
}
