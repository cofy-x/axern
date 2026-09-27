package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type reportReasonNodeClient struct {
	nodev1.NodeControlClient
	report *nodev1.ReportTunnelSessionStatusRequest
}

func (c *reportReasonNodeClient) ReportTunnelSessionStatus(_ context.Context, req *nodev1.ReportTunnelSessionStatusRequest, _ ...grpc.CallOption) (*nodev1.ReportTunnelSessionStatusResponse, error) {
	c.report = req
	// End the retry loop after capturing the exact request sent to controld.
	return nil, grpcstatus.Error(codes.FailedPrecondition, "terminal session")
}

type reportReasonTunnelClient struct {
	nodetunnelv1.AllocationTunnelClient
	err error
}

func (c reportReasonTunnelClient) ValidateAllocationTunnel(_ context.Context, req *nodetunnelv1.ValidateAllocationTunnelRequest, _ ...grpc.CallOption) (*nodetunnelv1.ValidateAllocationTunnelResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &nodetunnelv1.ValidateAllocationTunnelResponse{AllocationID: req.GetAllocationID()}, nil
}

func TestRunSessionReportsOnlySafeFailureReasons(t *testing.T) {
	privatePath := filepath.Join(t.TempDir(), "private-relay-ca.pem")
	privateTarget := "internal-relay.private"
	for _, tc := range []struct {
		name       string
		tunnelErr  error
		edgeTarget string
		caCert     string
		status     tunnelcontrolv1.TunnelSessionStatus
		reason     string
		sensitive  string
	}{
		{
			name:       "unwrapped malformed private relay target",
			edgeTarget: privateTarget,
			status:     tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED,
			reason:     "tunnel session setup failed",
			sensitive:  privateTarget,
		},
		{
			name:       "unwrapped private certificate path",
			edgeTarget: "relay.private:24100",
			caCert:     privatePath,
			status:     tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED,
			reason:     "tunnel session setup failed",
			sensitive:  privatePath,
		},
		{
			name:      "transient node validation with private detail",
			tunnelErr: grpcstatus.Error(codes.Unavailable, fmt.Sprintf("node socket at %s is unavailable", privatePath)),
			status:    tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED,
			reason:    allocationValidationRetryReason,
			sensitive: privatePath,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := &reportReasonNodeClient{}
			d := &daemon{
				nodeID: "node-test",
				node:   node,
				tunnel: reportReasonTunnelClient{err: tc.tunnelErr},
				relay:  relayConfig{caCert: tc.caCert},
			}
			d.runSession(context.Background(), &tunnelcontrolv1.TunnelSession{
				SessionID: "session-test", AllocationID: "alloc-test", RemotePort: 8765,
			}, "private-node-token", tc.edgeTarget)
			if node.report == nil || node.report.GetStatus() != tc.status || node.report.GetReason() != tc.reason {
				t.Fatalf("reported status/reason = %v, want %s / %q", node.report, tc.status, tc.reason)
			}
			if strings.Contains(node.report.GetReason(), tc.sensitive) || strings.Contains(node.report.GetReason(), "private-node-token") {
				t.Fatalf("public tunnel reason exposed an internal value: %q", node.report.GetReason())
			}
		})
	}
}

func TestSessionReportReasonRedactsWrappedRelayFailure(t *testing.T) {
	privateTarget := "10.23.45.67:24100"
	err := degradedSessionError(fmt.Errorf("dial tcp %s: connection refused", privateTarget))
	if got := sessionReportReason(err); got != "tunnel data plane temporarily unavailable" || strings.Contains(got, privateTarget) {
		t.Fatalf("reported degraded reason = %q", got)
	}
	if !strings.Contains(err.Error(), privateTarget) {
		t.Fatal("node-local wrapped cause was discarded")
	}
}
