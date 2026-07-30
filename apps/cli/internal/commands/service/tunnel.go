package service

import (
	"context"
	"fmt"
	"io"
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
	"github.com/spf13/cobra"
)

func tunnelCommand(runtime command.Runtime) *cobra.Command {
	var target, allocationID, nodeID string
	var ttl, readyTimeout, pingInterval, pongTimeout time.Duration
	var maxStreams int
	var noRenew bool
	cmd := &cobra.Command{Use: "tunnel <service-id>", Short: "Open a reverse TCP tunnel into a ready replica", Args: command.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		format, err := runtime.Format()
		if err != nil {
			return err
		}
		target = strings.TrimSpace(target)
		if target == "" {
			return command.Usage(fmt.Errorf("--to is required"))
		}
		if _, _, err := net.SplitHostPort(target); err != nil {
			return command.Usage(fmt.Errorf("--to must be host:port: %w", err))
		}
		connection, err := runtime.ResolveConnection()
		if err != nil {
			return command.Usage(err)
		}
		session, err := connection.Open(cmd.Context())
		if err != nil {
			return err
		}
		defer session.Close()
		signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return apptunnel.New(session.Clients.Tunnel).ForwardService(signalCtx, session.Clients.Service, apptunnel.ServiceForwardParams{CreateContext: session.Context, ServiceID: args[0], AllocationID: strings.TrimSpace(allocationID), NodeID: strings.TrimSpace(nodeID), LocalTarget: target, TTL: ttl, ReadyTimeout: readyTimeout, Relay: tunnelrelay.Config(connection.Config), RelayDialer: tunnelrelay.PeerDialer, OnReconnect: func(err error, backoff time.Duration) {
			fmt.Fprintf(cmd.ErrOrStderr(), "tunnel disconnected: %v; reconnecting in %s\n", err, backoff)
		}, Connector: apptunnel.ConnectorConfig{PingInterval: pingInterval, PongTimeout: pongTimeout, MaxStreams: maxStreams}, DisableRenew: noRenew, OnSessionCreated: func(session apptunnel.ServiceForwardSession) error {
			return renderServiceTunnelSession(cmd.OutOrStdout(), session, target, format)
		}})
	}}
	f := cmd.Flags()
	f.StringVar(&target, "to", "", "local upstream host:port")
	f.StringVar(&allocationID, "allocation-id", "", "specific ready allocation")
	f.StringVar(&nodeID, "node-id", "", "node containing the selected replica")
	f.DurationVar(&ttl, "ttl", 30*time.Minute, "session TTL")
	f.DurationVar(&readyTimeout, "ready-timeout", 30*time.Second, "bind readiness timeout")
	f.DurationVar(&pingInterval, "ping-interval", 15*time.Second, "connector ping interval")
	f.DurationVar(&pongTimeout, "pong-timeout", 45*time.Second, "connector pong timeout")
	f.IntVar(&maxStreams, "max-streams", 256, "maximum concurrent streams")
	f.BoolVar(&noRenew, "no-renew", false, "disable lease renewal")
	_ = f.MarkHidden("no-renew")
	return cmd
}

type serviceTunnelSessionJSON struct {
	ServiceID         string `json:"service_id"`
	AllocationID      string `json:"allocation_id"`
	NodeID            string `json:"node_id,omitempty"`
	SelectionReason   string `json:"selection_reason,omitempty"`
	ReadyReplicaCount int    `json:"ready_replica_count,omitempty"`
	SessionID         string `json:"session_id"`
	BoundAddr         string `json:"bound_addr,omitempty"`
	RemotePort        int32  `json:"remote_port,omitempty"`
	LocalTarget       string `json:"local_target"`
	RelayID           string `json:"relay_id,omitempty"`
	ClientEdgeTarget  string `json:"client_edge_target,omitempty"`
	NodeEdgeTarget    string `json:"node_edge_target,omitempty"`
}

func renderServiceTunnelSession(w io.Writer, result apptunnel.ServiceForwardSession, localTarget string, format output.Format) error {
	session := result.Session
	if session == nil {
		return fmt.Errorf("control plane returned empty tunnel session")
	}
	if format == output.FormatJSON {
		return output.PrintJSON(w, serviceTunnelSessionJSON{
			ServiceID:         result.ServiceID,
			AllocationID:      result.AllocationID,
			NodeID:            result.NodeID,
			SelectionReason:   result.SelectionReason,
			ReadyReplicaCount: result.ReadyReplicaCount,
			SessionID:         session.GetSessionID(),
			BoundAddr:         session.GetBoundAddr(),
			RemotePort:        session.GetRemotePort(),
			LocalTarget:       localTarget,
			RelayID:           session.GetRelayID(),
			ClientEdgeTarget:  session.GetClientEdgeTarget(),
			NodeEdgeTarget:    session.GetNodeEdgeTarget(),
		})
	}
	fmt.Fprintf(w, "Service: %s\n", result.ServiceID)
	fmt.Fprintf(w, "Selected allocation: %s\n", result.AllocationID)
	if result.NodeID != "" {
		fmt.Fprintf(w, "Selected node: %s\n", result.NodeID)
	}
	if result.SelectionReason != "" {
		fmt.Fprintf(w, "Selection: %s", result.SelectionReason)
		if result.ReadyReplicaCount > 1 {
			fmt.Fprintf(w, " among %d ready replicas", result.ReadyReplicaCount)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Tunnel session: %s\n", session.GetSessionID())
	fmt.Fprintf(w, "Local target: %s\n", localTarget)
	if session.GetBoundAddr() != "" {
		fmt.Fprintf(w, "Remote bind: %s\n", session.GetBoundAddr())
	} else {
		fmt.Fprintf(w, "Remote bind: 127.0.0.1:%d\n", session.GetRemotePort())
	}
	fmt.Fprintf(w, "Press Ctrl-C to revoke the tunnel.\n")
	return nil
}
