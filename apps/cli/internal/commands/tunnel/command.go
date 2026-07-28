package tunnelcmd

import (
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/spf13/cobra"
)

func Command(runtime command.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Manage allocation-scoped reverse TCP tunnels",
		Args:  command.NoArgs,
	}
	cmd.AddCommand(
		openCommand(runtime),
		getCommand(runtime),
		listCommand(runtime),
		revokeCommand(runtime),
		eventsCommand(runtime),
		inspectCommand(runtime),
		doctorCommand(runtime),
	)
	return cmd
}

type openOptions struct {
	allocationID string
	remotePort   int
	localTarget  string
	ttl          time.Duration
	readyTimeout time.Duration
	pingInterval time.Duration
	pongTimeout  time.Duration
	maxStreams   int
	waitReady    bool
}
