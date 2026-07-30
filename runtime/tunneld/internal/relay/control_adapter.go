package relay

import (
	"context"
	"time"

	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
)

type RelayControlAdapter struct {
	Client tunnelrelaycontrolv1.TunnelRelayControlClient
}

func (a RelayControlAdapter) ValidateTunnelPeer(ctx context.Context, in *tunnelrelaycontrolv1.ValidateTunnelPeerRequest) (*tunnelrelaycontrolv1.ValidateTunnelPeerResponse, error) {
	validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.Client.ValidateTunnelPeer(validateCtx, in)
}

func (a RelayControlAdapter) ReportTunnelPeerEvent(ctx context.Context, in *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error) {
	reportCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.Client.ReportTunnelPeerEvent(reportCtx, in)
}
