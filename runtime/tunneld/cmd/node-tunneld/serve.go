package main

import (
	"context"
	"fmt"
	"net"
	"time"

	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	allocationValidationFailureReason = "allocation tunnel authorization failed"
	allocationValidationRetryReason   = "allocation tunnel authorization temporarily unavailable"
)

func (d *daemon) serveSession(ctx context.Context, session *tunnelcontrolv1.TunnelSession, token, nodeEdgeTarget string) error {
	if err := d.validateAllocationTunnel(ctx, session.GetAllocationID()); err != nil {
		return err
	}
	return d.serveRunscSession(ctx, session, token, nodeEdgeTarget)
}

func (d *daemon) validateAllocationTunnel(ctx context.Context, allocationID string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	validated, err := d.tunnel.ValidateAllocationTunnel(checkCtx, &nodetunnelv1.ValidateAllocationTunnelRequest{AllocationID: allocationID})
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch grpcstatus.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Canceled:
			return sessionStatusError{status: tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, err: err, reason: allocationValidationRetryReason}
		}
		return failedSessionError(err, allocationValidationFailureReason)
	}
	if validated == nil || validated.GetAllocationID() != allocationID {
		return failedSessionError(fmt.Errorf("node tunnel authorization returned a different Allocation identity"), allocationValidationFailureReason)
	}
	return nil
}

func serverNameFromTarget(target string) (string, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	return host, nil
}
