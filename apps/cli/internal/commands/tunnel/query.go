package tunnelcmd

import (
	"context"
	"errors"
	"strings"
	"time"

	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	controltunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/spf13/cobra"
)

func getCommand(runtime command.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "get <session-id>",
		Short: "Get a tunnel session",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withControl(cmd, runtime, func(control apptunnel.Control, ctx context.Context) error {
				response, err := control.Get(ctx, args[0])
				if err != nil {
					return err
				}
				return renderSessions(cmd, runtime, []*controltunnelv1.TunnelSession{response.GetSession()}, false)
			})
		},
	}
}

func listCommand(runtime command.Runtime) *cobra.Command {
	var namespace, allocationID, nodeID string
	var includeTerminal bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tunnel sessions",
		Args:  command.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withControl(cmd, runtime, func(control apptunnel.Control, ctx context.Context) error {
				response, err := control.List(ctx, apptunnel.ListParams{Namespace: strings.TrimSpace(namespace), AllocationID: strings.TrimSpace(allocationID), NodeID: strings.TrimSpace(nodeID), IncludeTerminal: includeTerminal})
				if err != nil {
					return err
				}
				return renderSessions(cmd, runtime, response.GetSessions(), true)
			})
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace scope")
	cmd.Flags().StringVar(&allocationID, "allocation-id", "", "filter by allocation id")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "filter by node id")
	cmd.Flags().BoolVar(&includeTerminal, "include-terminal", false, "include revoked, expired, and failed sessions")
	return cmd
}

func revokeCommand(runtime command.Runtime) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "revoke <session-id>",
		Short: "Revoke a tunnel session",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return command.Usage(errors.New("--reason must not be empty"))
			}
			return withControl(cmd, runtime, func(control apptunnel.Control, ctx context.Context) error {
				response, err := control.Revoke(ctx, args[0], reason)
				if err != nil {
					return err
				}
				return renderSessions(cmd, runtime, []*controltunnelv1.TunnelSession{response.GetSession()}, false)
			})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "manual revoke", "revocation reason")
	return cmd
}

func eventsCommand(runtime command.Runtime) *cobra.Command {
	var limit int32
	cmd := &cobra.Command{
		Use:   "events <session-id>",
		Short: "List tunnel session events",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return command.Usage(errors.New("--limit must be positive"))
			}
			return withControl(cmd, runtime, func(control apptunnel.Control, ctx context.Context) error {
				response, err := control.Events(ctx, args[0], limit)
				if err != nil {
					return err
				}
				return renderEvents(cmd, runtime, response.GetEvents())
			})
		},
	}
	cmd.Flags().Int32Var(&limit, "limit", 50, "maximum events to return")
	return cmd
}

func inspectCommand(runtime command.Runtime) *cobra.Command {
	var limit int32
	cmd := &cobra.Command{
		Use:   "inspect <session-id>",
		Short: "Inspect a tunnel session and recent events",
		Args:  command.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return command.Usage(errors.New("--limit must be positive"))
			}
			return withControl(cmd, runtime, func(control apptunnel.Control, ctx context.Context) error {
				response, err := control.Inspect(ctx, args[0], limit)
				if err != nil {
					return err
				}
				format, err := runtime.Format()
				if err != nil {
					return err
				}
				if format == output.FormatJSON {
					return output.PrintJSON(cmd.OutOrStdout(), inspectDTO{Session: sessionDTOFromProto(response.GetSession()), Events: eventDTOs(response.GetEvents())})
				}
				output.RenderTunnel(cmd.OutOrStdout(), response.GetSession())
				output.RenderTunnelEvents(cmd.OutOrStdout(), response.GetEvents())
				return nil
			})
		},
	}
	cmd.Flags().Int32Var(&limit, "limit", 20, "maximum events to include")
	return cmd
}

func withControl(cmd *cobra.Command, runtime command.Runtime, action func(apptunnel.Control, context.Context) error) error {
	session, err := runtime.Open(cmd.Context())
	if err != nil {
		return err
	}
	defer session.Close()
	return action(apptunnel.New(session.Clients.Tunnel), session.Context)
}

type sessionDTO struct {
	SessionID        string `json:"session_id"`
	AllocationID     string `json:"allocation_id"`
	NodeID           string `json:"node_id,omitempty"`
	Status           string `json:"status"`
	RemotePort       int32  `json:"remote_port"`
	LocalTarget      string `json:"local_target,omitempty"`
	BoundAddr        string `json:"bound_addr,omitempty"`
	RelayID          string `json:"relay_id,omitempty"`
	ClientEdgeTarget string `json:"client_edge_target,omitempty"`
	NodeEdgeTarget   string `json:"node_edge_target,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
}

type eventDTO struct {
	CreatedAt  string `json:"created_at,omitempty"`
	EventType  string `json:"event_type"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	BoundAddr  string `json:"bound_addr,omitempty"`
}

type inspectDTO struct {
	Session sessionDTO `json:"session"`
	Events  []eventDTO `json:"events"`
}

func renderSessions(cmd *cobra.Command, runtime command.Runtime, sessions []*controltunnelv1.TunnelSession, list bool) error {
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	if format == output.FormatJSON {
		values := make([]sessionDTO, 0, len(sessions))
		for _, session := range sessions {
			if session != nil {
				values = append(values, sessionDTOFromProto(session))
			}
		}
		if list {
			return output.PrintJSON(cmd.OutOrStdout(), struct {
				Sessions []sessionDTO `json:"sessions"`
			}{Sessions: values})
		}
		if len(values) == 0 {
			return output.PrintJSON(cmd.OutOrStdout(), sessionDTO{})
		}
		return output.PrintJSON(cmd.OutOrStdout(), values[0])
	}
	if list {
		output.RenderTunnelTable(cmd.OutOrStdout(), sessions)
	} else if len(sessions) > 0 {
		output.RenderTunnel(cmd.OutOrStdout(), sessions[0])
	}
	return nil
}

func renderEvents(cmd *cobra.Command, runtime command.Runtime, events []*controltunnelv1.TunnelSessionEvent) error {
	format, err := runtime.Format()
	if err != nil {
		return err
	}
	if format == output.FormatJSON {
		return output.PrintJSON(cmd.OutOrStdout(), struct {
			Events []eventDTO `json:"events"`
		}{Events: eventDTOs(events)})
	}
	output.RenderTunnelEvents(cmd.OutOrStdout(), events)
	return nil
}

func sessionDTOFromProto(session *controltunnelv1.TunnelSession) sessionDTO {
	if session == nil {
		return sessionDTO{}
	}
	value := sessionDTO{
		SessionID: session.GetSessionID(), AllocationID: session.GetAllocationID(), NodeID: session.GetNodeID(),
		Status: enumValue(session.GetStatus().String(), "TUNNEL_SESSION_STATUS_"), RemotePort: session.GetRemotePort(),
		LocalTarget: session.GetLocalTarget(), BoundAddr: session.GetBoundAddr(), RelayID: session.GetRelayID(),
		ClientEdgeTarget: session.GetClientEdgeTarget(), NodeEdgeTarget: session.GetNodeEdgeTarget(), Reason: session.GetReason(),
	}
	if session.GetExpiresAt() != nil {
		value.ExpiresAt = session.GetExpiresAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	return value
}

func eventDTOs(events []*controltunnelv1.TunnelSessionEvent) []eventDTO {
	values := make([]eventDTO, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		value := eventDTO{
			EventType:  enumValue(event.GetEventType().String(), "TUNNEL_SESSION_EVENT_TYPE_"),
			Status:     enumValue(event.GetStatus().String(), "TUNNEL_SESSION_STATUS_"),
			ReasonCode: enumValue(event.GetReasonCode().String(), "TUNNEL_SESSION_EVENT_REASON_CODE_"),
			Reason:     event.GetReason(), BoundAddr: event.GetBoundAddr(),
		}
		if event.GetCreatedAt() != nil {
			value.CreatedAt = event.GetCreatedAt().AsTime().UTC().Format(time.RFC3339Nano)
		}
		values = append(values, value)
	}
	return values
}

func enumValue(value, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(value, prefix))
}
