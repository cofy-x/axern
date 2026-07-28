package tunnelcmd

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/apps/cli/internal/tunnelrelay"
	controltunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/spf13/cobra"
)

func openCommand(runtime command.Runtime) *cobra.Command {
	opts := openOptions{ttl: 30 * time.Minute, readyTimeout: 30 * time.Second, pingInterval: 15 * time.Second, pongTimeout: 45 * time.Second, maxStreams: 256, waitReady: true}
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open a foreground reverse TCP tunnel into an allocation",
		Args:  command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(opts.allocationID) == "" {
				return command.Usage(fmt.Errorf("--allocation-id is required"))
			}
			if strings.TrimSpace(opts.localTarget) == "" {
				return command.Usage(fmt.Errorf("--local is required"))
			}
			if _, _, err := net.SplitHostPort(opts.localTarget); err != nil {
				return command.Usage(fmt.Errorf("--local must be host:port: %w", err))
			}
			if cmd.Flags().Changed("remote-port") && (opts.remotePort < 1 || opts.remotePort > 65535) {
				return command.Usage(fmt.Errorf("--remote-port must be in 1..65535"))
			}
			if opts.ttl <= 0 || opts.readyTimeout <= 0 || opts.pingInterval < 0 || opts.pongTimeout <= 0 || opts.maxStreams <= 0 {
				return command.Usage(fmt.Errorf("ttl, ready-timeout, pong-timeout, and max-streams must be positive; ping-interval may be zero"))
			}
			return runForward(cmd, runtime, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.allocationID, "allocation-id", "", "target allocation id")
	f.IntVar(&opts.remotePort, "remote-port", 0, "port bound on 127.0.0.1 inside the allocation; omitted allocates automatically")
	f.StringVar(&opts.localTarget, "local", "", "local upstream host:port")
	f.DurationVar(&opts.ttl, "ttl", opts.ttl, "session TTL")
	f.DurationVar(&opts.readyTimeout, "ready-timeout", opts.readyTimeout, "node bind readiness timeout")
	f.DurationVar(&opts.pingInterval, "ping-interval", opts.pingInterval, "connector ping interval; 0 disables pings")
	f.DurationVar(&opts.pongTimeout, "pong-timeout", opts.pongTimeout, "connector pong timeout")
	f.IntVar(&opts.maxStreams, "max-streams", opts.maxStreams, "maximum concurrent streams")
	f.BoolVar(&opts.waitReady, "wait-ready", opts.waitReady, "wait for node bind readiness")
	return cmd
}

func runForward(cmd *cobra.Command, runtime command.Runtime, opts openOptions) error {
	connection, err := runtime.ConnectionConfig()
	if err != nil {
		return command.Usage(err)
	}
	session, err := runtime.Open(cmd.Context())
	if err != nil {
		return err
	}
	defer session.Close()
	var remotePort *int32
	if cmd.Flags().Changed("remote-port") {
		value := int32(opts.remotePort)
		remotePort = &value
	}
	signalCtx, stop := signal.NotifyContext(session.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return apptunnel.New(session.Clients.Tunnel).Forward(signalCtx, apptunnel.ForwardParams{
		CreateContext: session.Context,
		AllocationID:  strings.TrimSpace(opts.allocationID),
		RemotePort:    remotePort,
		LocalTarget:   strings.TrimSpace(opts.localTarget),
		TTL:           opts.ttl,
		WaitReady:     opts.waitReady,
		ReadyTimeout:  opts.readyTimeout,
		Relay:         tunnelrelay.Config(connection),
		RelayDialer:   tunnelrelay.PeerDialer,
		OnReconnect: func(err error, backoff time.Duration) {
			fmt.Fprintf(cmd.ErrOrStderr(), "tunnel disconnected: %v; reconnecting in %s\n", err, backoff)
		},
		Connector: apptunnel.ConnectorConfig{PingInterval: opts.pingInterval, PongTimeout: opts.pongTimeout, MaxStreams: opts.maxStreams},
		OnSessionCreated: func(result apptunnel.ForwardSession) error {
			return renderOpenSession(cmd, result.Session, opts.localTarget, runtime)
		},
	})
}

type openSessionDTO struct {
	SessionID        string `json:"session_id"`
	AllocationID     string `json:"allocation_id"`
	RemotePort       int32  `json:"remote_port"`
	BoundAddr        string `json:"bound_addr,omitempty"`
	LocalTarget      string `json:"local_target"`
	RelayID          string `json:"relay_id,omitempty"`
	ClientEdgeTarget string `json:"client_edge_target,omitempty"`
	NodeEdgeTarget   string `json:"node_edge_target,omitempty"`
}

func renderOpenSession(cmd *cobra.Command, session *controltunnelv1.TunnelSession, localTarget string, runtime command.Runtime) error {
	if session == nil {
		return fmt.Errorf("control plane returned an empty tunnel session")
	}
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	if format == output.FormatJSON {
		return output.PrintJSON(cmd.OutOrStdout(), openSessionDTO{
			SessionID: session.GetSessionID(), AllocationID: session.GetAllocationID(), RemotePort: session.GetRemotePort(),
			BoundAddr: session.GetBoundAddr(), LocalTarget: localTarget, RelayID: session.GetRelayID(),
			ClientEdgeTarget: session.GetClientEdgeTarget(), NodeEdgeTarget: session.GetNodeEdgeTarget(),
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Tunnel session: %s\n", session.GetSessionID())
	fmt.Fprintf(cmd.OutOrStdout(), "Allocation: %s\n", session.GetAllocationID())
	fmt.Fprintf(cmd.OutOrStdout(), "Local target: %s\n", localTarget)
	if session.GetBoundAddr() != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Remote bind: %s\n", session.GetBoundAddr())
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Remote bind: 127.0.0.1:%d\n", session.GetRemotePort())
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl-C to revoke the tunnel.")
	return nil
}
