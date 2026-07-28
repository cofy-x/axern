package output

import (
	"fmt"
	"io"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

func RenderTunnel(w io.Writer, session *tunnelcontrolv1.TunnelSession) {
	if session == nil {
		return
	}
	fmt.Fprintf(w, "Session:      %s\n", session.GetSessionID())
	fmt.Fprintf(w, "Allocation:   %s\n", session.GetAllocationID())
	fmt.Fprintf(w, "Node:         %s\n", session.GetNodeID())
	fmt.Fprintf(w, "Status:       %s\n", trimEnumPrefix(session.GetStatus().String(), "TUNNEL_SESSION_STATUS_"))
	fmt.Fprintf(w, "Remote:       127.0.0.1:%d\n", session.GetRemotePort())
	fmt.Fprintf(w, "Local:        %s\n", session.GetLocalTarget())
	fmt.Fprintf(w, "Relay:        %s\n", session.GetEdgeTarget())
	if session.GetNodeEdgeTarget() != "" && session.GetNodeEdgeTarget() != session.GetEdgeTarget() {
		fmt.Fprintf(w, "Node Relay:   %s\n", session.GetNodeEdgeTarget())
	}
	if session.GetBoundAddr() != "" {
		fmt.Fprintf(w, "Bound:        %s\n", session.GetBoundAddr())
	}
	if session.GetReason() != "" {
		fmt.Fprintf(w, "Reason:       %s\n", session.GetReason())
	}
	if session.GetExpiresAt() != nil {
		fmt.Fprintf(w, "Expires:      %s\n", session.GetExpiresAt().AsTime().Local().Format(time.RFC3339))
	}
}

func RenderTunnelTable(w io.Writer, sessions []*tunnelcontrolv1.TunnelSession) {
	rows := make([][]string, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		expires := ""
		if session.GetExpiresAt() != nil {
			expires = session.GetExpiresAt().AsTime().Local().Format(time.RFC3339)
		}
		rows = append(rows, []string{
			session.GetSessionID(),
			session.GetAllocationID(),
			trimEnumPrefix(session.GetStatus().String(), "TUNNEL_SESSION_STATUS_"),
			fmt.Sprintf("%d", session.GetRemotePort()),
			session.GetLocalTarget(),
			session.GetBoundAddr(),
			expires,
		})
	}
	RenderTable(w, []string{"SESSION", "ALLOCATION", "STATUS", "REMOTE", "LOCAL", "BOUND", "EXPIRES"}, rows)
}

func RenderTunnelEvents(w io.Writer, events []*tunnelcontrolv1.TunnelSessionEvent) {
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		created := ""
		if event.GetCreatedAt() != nil {
			created = event.GetCreatedAt().AsTime().Local().Format(time.RFC3339)
		}
		rows = append(rows, []string{
			created,
			trimEnumPrefix(event.GetEventType().String(), "TUNNEL_SESSION_EVENT_TYPE_"),
			trimEnumPrefix(event.GetStatus().String(), "TUNNEL_SESSION_STATUS_"),
			trimEnumPrefix(event.GetReasonCode().String(), "TUNNEL_SESSION_EVENT_REASON_CODE_"),
			event.GetReason(),
			event.GetBoundAddr(),
		})
	}
	RenderTable(w, []string{"TIME", "EVENT", "STATUS", "CODE", "REASON", "BOUND"}, rows)
}
