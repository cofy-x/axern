package tunnel

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type DoctorParams struct {
	SessionID     string
	AllocationID  string
	ServiceID     string
	LocalTarget   string
	Timeout       time.Duration
	ProbeRelay    func(context.Context, string, time.Duration) bool
	ServiceClient ServiceClient
}

type DoctorReport struct {
	ServiceID          string   `json:"service_id,omitempty"`
	SessionID          string   `json:"session_id,omitempty"`
	AllocationID       string   `json:"allocation_id,omitempty"`
	SelectedAllocation string   `json:"selected_allocation_id,omitempty"`
	SelectedNodeID     string   `json:"selected_node_id,omitempty"`
	Status             string   `json:"status,omitempty"`
	RelayID            string   `json:"relay_id,omitempty"`
	ClientTarget       string   `json:"client_edge_target,omitempty"`
	NodeTarget         string   `json:"node_edge_target,omitempty"`
	BoundAddr          string   `json:"bound_addr,omitempty"`
	ClientPeer         string   `json:"client_peer,omitempty"`
	NodePeer           string   `json:"node_peer,omitempty"`
	LastCloseReason    string   `json:"last_close_reason,omitempty"`
	Recommendation     string   `json:"recommendation,omitempty"`
	Checks             []string `json:"checks"`
	Problems           []string `json:"problems,omitempty"`
	RecentEvents       []string `json:"recent_events,omitempty"`
	LocalReachable     bool     `json:"local_reachable,omitempty"`
	RelayReachable     bool     `json:"relay_reachable,omitempty"`
	ControlReachable   bool     `json:"control_reachable"`
}

func (c Control) Doctor(ctx context.Context, params DoctorParams) (DoctorReport, error) {
	sessionID := strings.TrimSpace(params.SessionID)
	allocationID := strings.TrimSpace(params.AllocationID)
	serviceID := strings.TrimSpace(params.ServiceID)
	report := DoctorReport{ServiceID: serviceID, AllocationID: allocationID, ControlReachable: true}
	var session *tunnelv1.TunnelSession
	if sessionID != "" {
		resp, err := c.Inspect(ctx, sessionID, 20)
		if err != nil {
			return report, err
		}
		session = resp.GetSession()
		applyEventSummary(&report, resp.GetEvents())
	} else if serviceID != "" {
		if params.ServiceClient == nil {
			return report, fmt.Errorf("service client is required for service tunnel doctor")
		}
		replicas, err := params.ServiceClient.ListServiceReplicas(ctx, &servicev1.ListServiceReplicasRequest{
			ServiceID: serviceID,
			Filter: &servicev1.ServiceReplicaListFilter{
				View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT,
			},
		})
		if err != nil {
			return report, err
		}
		candidates := readyAllocationCandidates(serviceID, replicas.GetReplicas())
		if len(candidates) == 0 {
			report.Problems = append(report.Problems, fmt.Sprintf("service %s has no current ready replicas", serviceID))
			report.Recommendation = "wait for a ready service replica before opening or diagnosing a tunnel"
			return report, nil
		}
		selection := candidates[0]
		for _, candidate := range candidates {
			candidateSession, err := c.newestActiveSessionForAllocation(ctx, candidate.AllocationID)
			if err != nil {
				return report, err
			}
			if candidateSession != nil {
				selection = candidate
				session = candidateSession
				break
			}
		}
		report.SelectedAllocation = selection.AllocationID
		report.SelectedNodeID = selection.NodeID
		if session == nil {
			report.Problems = append(report.Problems, "no active tunnel sessions found for current ready service replicas")
			report.Recommendation = "start axern svc tunnel for this service, or pass --session-id for a known tunnel"
			return report, nil
		}
		events, _ := c.Events(ctx, session.GetSessionID(), 20)
		applyEventSummary(&report, events.GetEvents())
	} else {
		resp, err := c.List(ctx, ListParams{AllocationID: allocationID})
		if err != nil {
			return report, err
		}
		if len(resp.GetSessions()) == 0 {
			report.Problems = append(report.Problems, "no active tunnel sessions found for allocation")
			return report, nil
		}
		session = resp.GetSessions()[0]
		events, _ := c.Events(ctx, session.GetSessionID(), 20)
		applyEventSummary(&report, events.GetEvents())
	}
	if session != nil {
		report.SessionID = session.GetSessionID()
		report.AllocationID = session.GetAllocationID()
		report.Status = session.GetStatus().String()
		report.RelayID = session.GetRelayID()
		report.ClientTarget = firstNonEmpty(session.GetClientEdgeTarget(), session.GetEdgeTarget())
		report.NodeTarget = session.GetNodeEdgeTarget()
		report.BoundAddr = session.GetBoundAddr()
		if session.GetStatus() == tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING && strings.TrimSpace(session.GetBoundAddr()) != "" {
			report.Checks = append(report.Checks, "session is running and node bind is reported")
		} else {
			report.Problems = append(report.Problems, "session is not ready")
		}
		if params.ProbeRelay != nil {
			report.RelayReachable = params.ProbeRelay(ctx, report.ClientTarget, params.Timeout)
			if report.RelayReachable {
				report.Checks = append(report.Checks, "relay target is reachable")
			} else {
				report.Problems = append(report.Problems, "relay target is not reachable")
				report.Recommendation = "check the selected relay deployment/service and verify the session-bound relay target"
			}
		}
	}
	localTarget := strings.TrimSpace(params.LocalTarget)
	if localTarget != "" {
		report.LocalReachable = probeTCP(localTarget, params.Timeout)
		if report.LocalReachable {
			report.Checks = append(report.Checks, "local upstream is reachable")
		} else {
			report.Problems = append(report.Problems, "local upstream is not reachable")
			if report.Recommendation == "" {
				report.Recommendation = "start the local upstream or fix the --local target"
			}
		}
	}
	if report.Recommendation == "" {
		report.Recommendation = recommendationForPeers(report)
	}
	return report, nil
}

func (c Control) newestActiveSessionForAllocation(ctx context.Context, allocationID string) (*tunnelv1.TunnelSession, error) {
	resp, err := c.List(ctx, ListParams{AllocationID: allocationID})
	if err != nil {
		return nil, err
	}
	for _, session := range resp.GetSessions() {
		if session == nil {
			continue
		}
		switch session.GetStatus() {
		case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
			tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
			tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
			continue
		default:
			return session, nil
		}
	}
	return nil, nil
}

func applyEventSummary(report *DoctorReport, events []*tunnelv1.TunnelSessionEvent) {
	if report == nil {
		return
	}
	report.RecentEvents = summarizeEvents(events)
	for _, event := range events {
		eventType := event.GetEventType()
		reason := strings.TrimSpace(event.GetReason())
		switch eventType {
		case tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED:
			report.ClientPeer = "connected"
		case tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_DISCONNECTED:
			report.ClientPeer = "disconnected"
			report.LastCloseReason = firstNonEmpty(reason, event.GetReasonCode().String())
		case tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED:
			report.NodePeer = "connected"
		case tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_DISCONNECTED:
			report.NodePeer = "disconnected"
			report.LastCloseReason = firstNonEmpty(reason, event.GetReasonCode().String())
		}
	}
}

func summarizeEvents(events []*tunnelv1.TunnelSessionEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, fmt.Sprintf("%s %s %s", event.GetEventType().String(), event.GetReasonCode().String(), event.GetReason()))
	}
	return out
}

func recommendationForPeers(report DoctorReport) string {
	if report.SessionID == "" {
		return ""
	}
	if report.NodePeer == "" || report.NodePeer == "disconnected" {
		return "check node-tunneld on the selected node and verify the allocation is still running"
	}
	if report.ClientPeer == "" || report.ClientPeer == "disconnected" {
		return "check that the local foreground CLI connector is still running"
	}
	if len(report.Problems) == 0 {
		return "tunnel checks passed"
	}
	return "inspect recent tunnel events and relay/node logs for the failing component"
}

func probeTCP(target string, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", target)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
