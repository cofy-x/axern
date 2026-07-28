package relay

import (
	"context"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
)

type TunnelControlAdapter struct {
	TunnelClient tunnelcontrolv1.TunnelControlClient
	RelayClient  tunnelrelaycontrolv1.TunnelRelayControlClient
}

func (a TunnelControlAdapter) ValidateTunnelPeer(ctx context.Context, in *tunnelcontrolv1.ValidateTunnelPeerRequest) (*tunnelcontrolv1.ValidateTunnelPeerResponse, error) {
	validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.TunnelClient.ValidateTunnelPeer(validateCtx, in)
}

func (a TunnelControlAdapter) ReportTunnelPeerEvent(ctx context.Context, in *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error) {
	reportCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.RelayClient.ReportTunnelPeerEvent(reportCtx, in)
}
