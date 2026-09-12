package tunnel

import (
	"context"
	"strings"
	"testing"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

func TestDoctorAllocationSelectsActiveSessionAndReportsPeers(t *testing.T) {
	client := &fakeTunnelClient{
		listResp: &tunnelv1.ListTunnelSessionsResponse{Sessions: []*tunnelv1.TunnelSession{{
			SessionID:        "tun-1",
			AllocationID:     "alloc-a",
			Status:           tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
			RelayID:          "relay-a",
			ClientEdgeTarget: "127.0.0.1:24317",
			NodeEdgeTarget:   "tunneld-a.axern-local.svc.cluster.local:24100",
			BoundAddr:        "127.0.0.1:41000",
		}}},
		eventsResp: &tunnelv1.ListTunnelSessionEventsResponse{Events: []*tunnelv1.TunnelSessionEvent{
			{EventType: tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED},
			{EventType: tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED},
		}},
	}
	report, err := New(client).Doctor(context.Background(), DoctorParams{
		AllocationID: "alloc-a",
		ProbeRelay:   func(context.Context, string, time.Duration) bool { return true },
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if report.AllocationID != "alloc-a" || report.SessionID != "tun-1" {
		t.Fatalf("unexpected allocation selection: %+v", report)
	}
	if report.ClientPeer != "connected" || report.NodePeer != "connected" || report.Recommendation != "tunnel checks passed" {
		t.Fatalf("unexpected peer summary: %+v", report)
	}
}

func TestDoctorRelayUnavailableRecommendation(t *testing.T) {
	client := &fakeTunnelClient{inspectResp: &tunnelv1.InspectTunnelSessionResponse{
		Session: &tunnelv1.TunnelSession{
			SessionID:        "tun-1",
			AllocationID:     "alloc-a",
			Status:           tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
			ClientEdgeTarget: "127.0.0.1:24317",
			BoundAddr:        "127.0.0.1:41000",
		},
	}}
	report, err := New(client).Doctor(context.Background(), DoctorParams{
		SessionID:  "tun-1",
		ProbeRelay: func(context.Context, string, time.Duration) bool { return false },
	})
	if err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}
	if !strings.Contains(report.Recommendation, "relay") {
		t.Fatalf("recommendation = %q, want relay guidance", report.Recommendation)
	}
}
