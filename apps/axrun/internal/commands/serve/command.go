package serve

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/application/server"
	"github.com/cofy-x/axern/apps/axrun/internal/command"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var config server.Config
	cmd := &cobra.Command{
		Use: "serve", Short: "Serve the rollout application API", Args: command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := approllout.Service{AgentRegistry: agentcatalog.DefaultRegistry()}
			srv := server.New(config, svc)
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			fmt.Fprintf(cmd.OutOrStdout(), "listening=:%d\n", config.Port)
			return srv.ListenAndServe(ctx)
		},
	}
	cmd.Flags().IntVarP(&config.Port, "port", "p", 8080, "HTTP port")
	cmd.Flags().IntVar(&config.MaxRollouts, "max-rollouts", 4, "maximum concurrent rollouts")
	cmd.Flags().StringVar(&config.Output, "output-dir", ".axrun/runs", "run output directory")
	cmd.Flags().StringVar(&config.AuthToken, "auth-token", os.Getenv("AXRUN_SERVER_AUTH_TOKEN"), "bearer token")
	return cmd
}
